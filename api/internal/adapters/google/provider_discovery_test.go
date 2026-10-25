package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderNewPerformsNoDiscovery(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	if provider == nil || provider.client.Timeout != requestTimeout {
		t.Fatal("provider not constructed")
	}
	if requests.Load() != 0 {
		t.Fatal("construction performed discovery requests")
	}
}

func newDiscoveryServer(t *testing.T, up *atomic.Bool, discoveries *atomic.Int32) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveries.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if !up.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProviderDiscoveryCachedOnce(t *testing.T) {
	var up atomic.Bool
	var discoveries atomic.Int32
	up.Store(true)
	server := newDiscoveryServer(t, &up, &discoveries)
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			authorization, err := provider.AuthorizationURL(context.Background(), "state", "nonce", "verifier")
			if err != nil || !strings.HasPrefix(authorization, server.URL+"/authorize") {
				t.Errorf("authorization URL %q error %v", authorization, err)
			}
		}()
	}
	wg.Wait()
	if discoveries.Load() != 1 {
		t.Fatalf("discovery requests %d, want 1", discoveries.Load())
	}
	if _, err := provider.AuthorizationURL(context.Background(), "state", "nonce", "verifier"); err != nil || discoveries.Load() != 1 {
		t.Fatalf("successful discovery not cached: requests %d error %v", discoveries.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.AuthorizationURL(ctx, "state", "nonce", "verifier"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached provider lost cancellation: %v", err)
	}
}

func TestProviderDiscoveryFailureNotCached(t *testing.T) {
	var up atomic.Bool
	var discoveries atomic.Int32
	server := newDiscoveryServer(t, &up, &discoveries)
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	if _, err := provider.AuthorizationURL(context.Background(), "state", "nonce", "verifier"); err == nil {
		t.Fatal("failing discovery ignored")
	}
	if _, err := provider.Exchange(context.Background(), "code", "verifier", "nonce"); err == nil {
		t.Fatal("failing discovery ignored on exchange")
	}
	if discoveries.Load() != 2 {
		t.Fatalf("failed discovery not retried: requests %d", discoveries.Load())
	}
	up.Store(true)
	authorization, err := provider.AuthorizationURL(context.Background(), "state", "nonce", "verifier")
	if err != nil || !strings.HasPrefix(authorization, server.URL+"/authorize") {
		t.Fatalf("discovery did not recover: %q error %v", authorization, err)
	}
	if discoveries.Load() != 3 {
		t.Fatalf("requests %d, want 3", discoveries.Load())
	}
}

func TestProviderDiscoveryCanceledInFlight(t *testing.T) {
	var up atomic.Bool
	entered := make(chan struct{}, 1)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"jwks_uri":                              server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer server.Close()
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := provider.AuthorizationURL(ctx, "state", "nonce", "verifier")
		result <- err
	}()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight cancellation lost: %v", err)
	}
	up.Store(true)
	authorization, err := provider.AuthorizationURL(context.Background(), "state", "nonce", "verifier")
	if err != nil || !strings.HasPrefix(authorization, server.URL+"/authorize") {
		t.Fatalf("discovery did not recover after cancellation: %q error %v", authorization, err)
	}
}

func TestProviderDiscoveryContextCanceled(t *testing.T) {
	var up atomic.Bool
	var discoveries atomic.Int32
	server := newDiscoveryServer(t, &up, &discoveries)
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.AuthorizationURL(ctx, "state", "nonce", "verifier"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if discoveries.Load() != 0 {
		t.Fatal("network attempted with canceled context")
	}
}

func TestProviderDiscoveryWaiterCancellation(t *testing.T) {
	guard, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	provider := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := provider.AuthorizationURL(firstCtx, "s1", "n1", "v1")
		firstResult <- err
	}()
	<-entered
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		_, err := provider.AuthorizationURL(secondCtx, "s2", "n2", "v2")
		secondResult <- err
	}()
	<-secondStarted
	cancelSecond()
	var secondErr error
	select {
	case secondErr = <-secondResult:
	case <-time.After(200 * time.Millisecond):
		cancelFirst()
		<-firstResult
		<-secondResult
		t.Fatal("canceled waiter blocked behind in-flight discovery")
	case <-guard.Done():
		t.Fatal("test guard expired")
	}
	if !errors.Is(secondErr, context.Canceled) {
		cancelFirst()
		<-firstResult
		t.Fatalf("canceled waiter error %v, want context.Canceled", secondErr)
	}
	cancelFirst()
	if err := <-firstResult; err == nil {
		t.Fatal("in-flight discovery unexpectedly succeeded")
	}
}

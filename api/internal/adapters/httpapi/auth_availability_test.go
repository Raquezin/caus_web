package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/adapters/google"
	"caus-platform/api/internal/domain"
)

func TestGoogleLoginUnavailableAndRecoveryE2E(t *testing.T) {
	origin := "https://app.example:8443"
	var discoveries atomic.Int32
	var up atomic.Bool
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		discoveries.Add(1)
		if !up.Load() {
			writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                issuer.URL,
			"authorization_endpoint":                issuer.URL + "/authorize",
			"token_endpoint":                        issuer.URL + "/token",
			"jwks_uri":                              issuer.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(issuer.Close)
	provider := google.New(context.Background(), issuer.URL, "test-client", "secret-marker", origin+"/api/auth/google/callback")
	if discoveries.Load() != 0 {
		t.Fatal("provider construction performed discovery")
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	auth := NewAuth(provider, nil, AuthConfig{PublicURL: origin, SessionTTL: 720 * time.Hour}, logger)
	server := httptest.NewServer(NewRouter(people, func(context.Context) error { return nil }, logger, auth))
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(server.Close)
	if discoveries.Load() != 0 {
		t.Fatal("router construction performed discovery")
	}
	publicPaths := []string{"/api/healthz", "/api/readyz"}
	checkPublic := func() {
		t.Helper()
		for _, path := range publicPaths {
			req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", origin)
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s: status %d", path, response.StatusCode)
			}
			if response.Header.Get("Access-Control-Allow-Origin") != origin {
				t.Fatalf("%s: allow-origin %q", path, response.Header.Get("Access-Control-Allow-Origin"))
			}
		}
	}
	login := func() *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/auth/google/login", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	checkPublic()
	response := login()
	code, _ := errorBody(t, response)
	if response.StatusCode != http.StatusServiceUnavailable || code != "auth_unavailable" ||
		response.Header.Get("Set-Cookie") != "" || response.Header.Get("Location") != "" {
		t.Fatalf("status %d code %s", response.StatusCode, code)
	}
	second := login()
	second.Body.Close()
	if second.StatusCode != http.StatusServiceUnavailable || discoveries.Load() != 2 {
		t.Fatalf("failed discovery not retried: status %d discoveries %d", second.StatusCode, discoveries.Load())
	}
	for _, secret := range []string{"secret-marker", issuer.URL} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("sensitive value %q leaked to logs", secret)
		}
	}
	checkPublic()
	up.Store(true)
	response = login()
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") == "" || len(response.Cookies()) != 1 {
		t.Fatalf("recovery: status %d location %q cookies %d", response.StatusCode, response.Header.Get("Location"), len(response.Cookies()))
	}
	response.Body.Close()
	if discoveries.Load() != 3 {
		t.Fatalf("discoveries %d, want 3", discoveries.Load())
	}
	response = login()
	response.Body.Close()
	if response.StatusCode != http.StatusFound || discoveries.Load() != 3 {
		t.Fatal("successful discovery not cached")
	}
	checkPublic()
}

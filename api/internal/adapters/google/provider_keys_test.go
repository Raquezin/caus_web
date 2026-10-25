package google

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"caus-platform/api/internal/domain"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderJWKSUnavailableAndRecovery(t *testing.T) {
	provider, _, _ := mockProvider(t, oidcTestCase{})
	var keysHits atomic.Int32
	var mode atomic.Int32
	base := provider.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	provider.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/keys" {
			keysHits.Add(1)
			switch mode.Load() {
			case 1:
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody,
					Header: make(http.Header), Request: r}, nil
			case 2:
				return nil, errors.New("keys transport failure")
			case 3:
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("not json")),
					Header: make(http.Header), Request: r}, nil
			}
		}
		return base.RoundTrip(r)
	})
	for _, failure := range []int32{1, 2, 3} {
		mode.Store(failure)
		if _, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce"); !errors.Is(err, domain.ErrAuthUnavailable) || errors.Is(err, domain.ErrInvalidIdentity) {
			t.Fatalf("JWKS failure mode %d misclassified: %v", failure, err)
		}
	}
	if keysHits.Load() == 0 {
		t.Fatal("/keys decorator never invoked")
	}
	mode.Store(0)
	identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
	if err != nil || identity.Subject != "verified-subject" {
		t.Fatalf("provider did not recover after JWKS outage: %v", err)
	}
}

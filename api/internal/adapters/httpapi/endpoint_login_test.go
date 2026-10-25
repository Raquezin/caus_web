package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

type endpointLoginProvider struct {
	mu        sync.Mutex
	calls     int
	states    []string
	nonces    []string
	verifiers []string
}

func (p *endpointLoginProvider) AuthorizationURL(_ context.Context, state, nonce, verifier string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.states = append(p.states, state)
	p.nonces = append(p.nonces, nonce)
	p.verifiers = append(p.verifiers, verifier)
	query := url.Values{
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {endpointLoginChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	return "https://accounts.example/authorize?" + query.Encode(), nil
}

func (p *endpointLoginProvider) Exchange(context.Context, string, string, string) (domain.GoogleIdentity, error) {
	return domain.GoogleIdentity{}, domain.ErrAuthUnavailable
}

func (p *endpointLoginProvider) snapshot() (int, []string, []string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, append([]string(nil), p.states...), append([]string(nil), p.nonces...), append([]string(nil), p.verifiers...)
}

func endpointLoginChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newEndpointLoginServer(t *testing.T, publicURL string) (*httptest.Server, *endpointLoginProvider) {
	t.Helper()
	provider := &endpointLoginProvider{}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, nil, AuthConfig{PublicURL: publicURL, SessionTTL: time.Hour}, logger)
	server := httptest.NewServer(NewRouter(&fakePeople{}, nil, logger, auth))
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(server.Close)
	return server, provider
}

func TestEndpointLoginGeneratesDistinctUnpredictableSecrets(t *testing.T) {
	server, provider := newEndpointLoginServer(t, "https://club.example")
	const requests = 16
	seen := make(map[string]bool, requests*3)
	for i := 0; i < requests; i++ {
		response := do(t, server, http.MethodGet, "/api/auth/google/login")
		if response.StatusCode != http.StatusFound {
			t.Fatalf("request %d: status %d", i, response.StatusCode)
		}
		cookie, flow := loginFlow(t, response)
		response.Body.Close()
		if cookie.Name != "__Host-caus_oauth" || cookie.Value == "" {
			t.Fatalf("request %d: missing flow cookie", i)
		}
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			t.Fatalf("request %d: invalid authorization URL: %v", i, err)
		}
		query := location.Query()
		if query.Get("state") != flow.State || query.Get("nonce") != flow.Nonce ||
			query.Get("code_challenge_method") != "S256" ||
			query.Get("code_challenge") != endpointLoginChallenge(flow.Verifier) {
			t.Fatalf("request %d: authorization URL does not match flow secrets", i)
		}
		if flow.State == flow.Nonce || flow.State == flow.Verifier || flow.Nonce == flow.Verifier {
			t.Fatalf("request %d: state, nonce and PKCE verifier are not distinct", i)
		}
		for _, secret := range []string{flow.State, flow.Nonce, flow.Verifier} {
			if !validOAuthRandom(secret) {
				t.Fatalf("request %d: secret is not 256-bit randomness", i)
			}
			if seen[secret] {
				t.Fatalf("request %d: secret reused across requests", i)
			}
			seen[secret] = true
		}
	}
	calls, states, nonces, verifiers := provider.snapshot()
	if calls != requests || len(states) != requests || len(nonces) != requests || len(verifiers) != requests {
		t.Fatalf("provider calls %d, states %d, nonces %d, verifiers %d", calls, len(states), len(nonces), len(verifiers))
	}
}

func TestEndpointLoginFlowCookieSecurityFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		publicURL  string
		cookieName string
		path       string
		secure     bool
	}{
		{name: "https", publicURL: "https://clubalgoritmiaus.es", cookieName: "__Host-caus_oauth", path: "/", secure: true},
		{name: "http", publicURL: "http://localhost:8080", cookieName: "caus_oauth", path: "/api/auth/google", secure: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := newEndpointLoginServer(t, tc.publicURL)
			response := do(t, server, http.MethodGet, "/api/auth/google/login")
			defer response.Body.Close()
			if response.StatusCode != http.StatusFound {
				t.Fatalf("status %d", response.StatusCode)
			}
			cookie, _ := loginFlow(t, response)
			if !cookie.HttpOnly || cookie.Secure != tc.secure || cookie.SameSite != http.SameSiteLaxMode ||
				cookie.Domain != "" || cookie.Name != tc.cookieName || cookie.Path != tc.path || cookie.MaxAge != 600 {
				t.Fatalf("unsafe flow cookie flags: %+v", cookie)
			}
		})
	}
}

func TestEndpointLoginPreservesSafeReturnTo(t *testing.T) {
	server, _ := newEndpointLoginServer(t, "https://club.example")
	for _, returnTo := range []string{
		"/", "/cuenta/?tab=profile", "/en/news/some%20post", "/discounts/100%25", "/encoded%3Fpart%25",
	} {
		response := do(t, server, http.MethodGet, "/api/auth/google/login?return_to="+url.QueryEscape(returnTo))
		if response.StatusCode != http.StatusFound {
			t.Fatalf("return_to %q: status %d", returnTo, response.StatusCode)
		}
		_, flow := loginFlow(t, response)
		response.Body.Close()
		if flow.ReturnTo != returnTo {
			t.Fatalf("return_to %q changed to %q", returnTo, flow.ReturnTo)
		}
	}
	response := do(t, server, http.MethodGet, "/api/auth/google/login")
	defer response.Body.Close()
	_, flow := loginFlow(t, response)
	if flow.ReturnTo != "/" {
		t.Fatalf("default return_to %q", flow.ReturnTo)
	}
}

func TestEndpointLoginRejectsUnsafeEncodedRedirectsBeforeProvider(t *testing.T) {
	server, provider := newEndpointLoginServer(t, "https://club.example")
	for _, query := range []string{
		"return_to=%2F%2Fevil.example",
		"return_to=%2F%5Cevil.example",
		"return_to=%2F%2fevil.example",
		"return_to=%2F%252fevil.example",
		"return_to=%252F%252Fevil.example",
		"return_to=%2F%255cevil.example",
		"return_to=https%3A%2F%2Fevil.example",
		"return_to=%2F%250d%250aLocation%3Ahttps%3A%2F%2Fevil.example",
		"return_to=/&return_to=/safe",
		"return_to=%ZZ",
	} {
		response := do(t, server, http.MethodGet, "/api/auth/google/login?"+query)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q: status %d", query, response.StatusCode)
		}
		code, _ := errorBody(t, response)
		if code != "invalid_return_to" {
			t.Fatalf("query %q: code %s", query, code)
		}
		if response.Header.Get("Set-Cookie") != "" || response.Header.Get("Location") != "" {
			t.Fatalf("query %q: rejected redirect produced a cookie or location", query)
		}
	}
	if calls, _, _, _ := provider.snapshot(); calls != 0 {
		t.Fatalf("provider consulted %d times before redirect validation", calls)
	}
}

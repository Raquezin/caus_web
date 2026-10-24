package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"caus-platform/api/internal/adapters/google"
)

func newLoginServer(t *testing.T, publicURL string) (*httptest.Server, *bytes.Buffer, string) {
	t.Helper()
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Errorf("unexpected provider request %s", r.URL.Path)
			http.NotFound(w, r)
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
	provider, err := google.New(context.Background(), issuer.URL, "test-client", "secret-marker", publicURL+"/api/auth/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	auth := NewAuth(provider, nil, AuthConfig{PublicURL: publicURL, SessionTTL: 720 * time.Hour}, logger)
	server := httptest.NewServer(NewRouter(&fakePeople{}, nil, logger, auth))
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(server.Close)
	return server, &logs, issuer.URL
}

func loginFlow(t *testing.T, response *http.Response) (*http.Cookie, oauthFlow) {
	t.Helper()
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != "caus_oauth" {
		t.Fatalf("expected one flow cookie, got %d", len(cookies))
	}
	flow, err := decodeOAuthFlow(cookies[0].Value, time.Now())
	if err != nil {
		t.Fatal("invalid flow cookie")
	}
	return cookies[0], flow
}

func TestGoogleLoginE2E(t *testing.T) {
	for _, publicURL := range []string{"https://clubalgoritmiaus.es", "http://localhost:8080"} {
		t.Run(publicURL, func(t *testing.T) {
			server, logs, issuer := newLoginServer(t, publicURL)
			before := time.Now()
			response := do(t, server, http.MethodGet, "/api/auth/google/login")
			defer response.Body.Close()
			if response.StatusCode != http.StatusFound {
				t.Fatalf("status %d", response.StatusCode)
			}
			cookie, flow := loginFlow(t, response)
			if flow.ReturnTo != "/" || flow.State == flow.Nonce || flow.State == flow.Verifier || flow.Nonce == flow.Verifier {
				t.Fatal("invalid return path or non-independent secrets")
			}
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" ||
				cookie.Path != "/api/auth/google" || cookie.MaxAge != 600 ||
				cookie.Secure != strings.HasPrefix(publicURL, "https://") {
				t.Fatal("incorrect flow cookie flags")
			}
			if cookie.Expires.Unix() != flow.ExpiresAt || flow.ExpiresAt < before.Add(oauthFlowTTL).Unix() ||
				flow.ExpiresAt > time.Now().Add(oauthFlowTTL).Unix() {
				t.Fatal("incorrect cookie expiry")
			}
			location, err := url.Parse(response.Header.Get("Location"))
			if err != nil || location.Scheme+"://"+location.Host+location.Path != issuer+"/authorize" {
				t.Fatal("incorrect authorization endpoint")
			}
			query := location.Query()
			challenge := sha256.Sum256([]byte(flow.Verifier))
			want := map[string]string{
				"client_id": "test-client", "response_type": "code", "scope": "openid email profile",
				"redirect_uri": publicURL + "/api/auth/google/callback", "state": flow.State, "nonce": flow.Nonce,
				"code_challenge_method": "S256", "code_challenge": base64.RawURLEncoding.EncodeToString(challenge[:]),
			}
			if len(query) != len(want) {
				t.Fatalf("unexpected authorization parameters: %d", len(query))
			}
			for key, value := range want {
				if len(query[key]) != 1 || query.Get(key) != value {
					t.Fatalf("incorrect authorization parameter %s", key)
				}
			}
			if response.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
			for _, secret := range []string{flow.State, flow.Nonce, flow.Verifier, cookie.Value, "secret-marker"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("secret leaked to logs")
				}
			}
			t.Log("local OIDC discovery -> HTTP login -> 302 authorization URL, state/nonce/PKCE and cookie verified")
		})
	}
}

func TestGoogleLoginRandomness(t *testing.T) {
	server, _, _ := newLoginServer(t, "http://localhost:8080")
	seen := make(map[string]bool)
	for i := 0; i < 5; i++ {
		response := do(t, server, http.MethodGet, "/api/auth/google/login")
		_, flow := loginFlow(t, response)
		response.Body.Close()
		for _, value := range []string{flow.State, flow.Nonce, flow.Verifier} {
			if seen[value] || !validOAuthRandom(value) {
				t.Fatal("reused or invalid OAuth random value")
			}
			seen[value] = true
		}
	}
}

func TestGoogleLoginReturnTo(t *testing.T) {
	server, _, _ := newLoginServer(t, "http://localhost:8080")
	for _, value := range []string{"/", "/cuenta/", "/news/?page=2&sort=date", "/en/news/some%20post"} {
		response := do(t, server, http.MethodGet, "/api/auth/google/login?return_to="+url.QueryEscape(value))
		if response.StatusCode != http.StatusFound {
			t.Fatalf("valid return_to %q: status %d", value, response.StatusCode)
		}
		_, flow := loginFlow(t, response)
		response.Body.Close()
		if flow.ReturnTo != value {
			t.Fatalf("return path changed: %q", flow.ReturnTo)
		}
	}
}

func TestGoogleLoginRejectsReturnTo(t *testing.T) {
	server, _, _ := newLoginServer(t, "http://localhost:8080")
	queries := []string{"return_to=/&return_to=/other", "return_to=%ZZ", "x=%ZZ", "return_to=/;x=1"}
	for _, value := range []string{
		"", "relative", "https://evil.example", "javascript:alert(1)", "//evil.example",
		"///evil.example", "\\evil.example", "/\\evil.example", "/safe\\path",
		"/%2fevil.example", "/%252fevil.example", "/%5cevil.example", "/%255cevil.example",
		"/%0d%0aLocation:https://evil.example", "/\n", "/\r", "/\x00", "/\x7f", "/\u0085",
		"/%00", "/%ZZ", "/%2500", strings.Repeat("/", maxReturnToLength+1),
	} {
		queries = append(queries, "return_to="+url.QueryEscape(value))
	}
	for _, query := range queries {
		response := do(t, server, http.MethodGet, "/api/auth/google/login?"+query)
		if response.StatusCode != http.StatusBadRequest || response.Header.Get("Set-Cookie") != "" || response.Header.Get("Location") != "" {
			t.Fatalf("query %q: status %d or cookie/redirect on error", query, response.StatusCode)
		}
		code, _ := errorBody(t, response)
		if code != "invalid_return_to" {
			t.Fatalf("error code %s", code)
		}
	}
}

func TestGoogleLoginMethods(t *testing.T) {
	server, _, _ := newLoginServer(t, "http://localhost:8080")
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		response := do(t, server, method, "/api/auth/google/login")
		code, _ := errorBody(t, response)
		if response.StatusCode != http.StatusMethodNotAllowed || code != "method_not_allowed" ||
			response.Header.Get("Allow") != "GET, HEAD" || response.Header.Get("Set-Cookie") != "" {
			t.Fatalf("method %s: status %d code %s", method, response.StatusCode, code)
		}
	}
	response := do(t, server, http.MethodHead, "/api/auth/google/login?return_to=/cuenta/")
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 0 || response.StatusCode != http.StatusFound || response.Header.Get("Location") == "" {
		t.Fatalf("HEAD: status %d body length %d error %v", response.StatusCode, len(body), err)
	}
	_, flow := loginFlow(t, response)
	if flow.ReturnTo != "/cuenta/" {
		t.Fatal("HEAD return path not preserved")
	}
}

func TestGoogleLoginOnlyRouteRegistered(t *testing.T) {
	server, _, _ := newLoginServer(t, "http://localhost:8080")
	for _, path := range []string{"/api/auth/google/callback", "/api/auth/logout", "/api/me"} {
		response := do(t, server, http.MethodGet, path)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("out-of-scope route %s registered", path)
		}
	}
	withoutAuth, _ := newTestServer(&fakePeople{}, nil)
	defer withoutAuth.Close()
	response := do(t, withoutAuth, http.MethodGet, "/api/auth/google/login")
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("login registered without auth dependency")
	}
}

func TestDecodeOAuthFlow(t *testing.T) {
	now := time.Unix(1700000000, 0)
	valid := oauthFlow{
		State: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: "/cuenta/?tab=profile", ExpiresAt: now.Add(oauthFlowTTL).Unix(),
	}
	encode := func(flow oauthFlow) string {
		data, err := json.Marshal(flow)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	if got, err := decodeOAuthFlow(encode(valid), now); err != nil || got != valid {
		t.Fatal("valid flow rejected")
	}
	for name, change := range map[string]func(*oauthFlow){
		"expired":             func(flow *oauthFlow) { flow.ExpiresAt = now.Unix() - 1 },
		"exact expiry":        func(flow *oauthFlow) { flow.ExpiresAt = now.Unix() },
		"far future":          func(flow *oauthFlow) { flow.ExpiresAt++ },
		"missing state":       func(flow *oauthFlow) { flow.State = "" },
		"invalid nonce":       func(flow *oauthFlow) { flow.Nonce = "invalid" },
		"invalid verifier":    func(flow *oauthFlow) { flow.Verifier = strings.Repeat("x", 43) },
		"unsafe return path":  func(flow *oauthFlow) { flow.ReturnTo = "//evil.example" },
		"encoded return path": func(flow *oauthFlow) { flow.ReturnTo = "/%252fevil.example" },
	} {
		t.Run(name, func(t *testing.T) {
			flow := valid
			change(&flow)
			if _, err := decodeOAuthFlow(encode(flow), now); err == nil {
				t.Fatal("invalid flow accepted")
			}
		})
	}
	for _, value := range []string{
		"", "%%%", strings.Repeat("x", maxOAuthCookieLength+1),
		base64.RawURLEncoding.EncodeToString([]byte(`{"unknown":true}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`null`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"state":`)),
		base64.RawURLEncoding.EncodeToString(append([]byte(`{}`), []byte(`{}`)...)),
	} {
		if _, err := decodeOAuthFlow(value, now); err == nil {
			t.Fatal("malformed flow accepted")
		}
	}
	if _, err := decodeOAuthFlow(encode(valid), time.Unix(valid.ExpiresAt, 0)); err == nil {
		t.Fatal("flow accepted at exact expiration boundary")
	}
}

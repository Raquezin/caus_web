package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"caus-platform/api/internal/domain"
)

type oidcTestCase struct {
	mutate           func(map[string]any)
	invalidSignature bool
	missingIDToken   bool
	tokenError       bool
	tls              bool
}

func mockProvider(t *testing.T, tc oidcTestCase) (*Provider, string, *atomic.Int32) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signingKey := key
	if tc.invalidSignature {
		signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	var tokenRequests atomic.Int32
	var server *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                server.URL,
				"authorization_endpoint":                server.URL + "/authorize",
				"token_endpoint":                        server.URL + "/token",
				"jwks_uri":                              server.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
				{Key: &key.PublicKey, KeyID: "test-key", Use: "sig", Algorithm: string(jose.RS256)},
			}})
		case "/token":
			tokenRequests.Add(1)
			if r.Method != http.MethodPost {
				t.Errorf("token method %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for name, want := range map[string]string{
				"code": "authorization-code", "code_verifier": "expected-verifier",
				"grant_type": "authorization_code", "redirect_uri": "http://localhost:8080/api/auth/google/callback",
			} {
				if r.PostForm.Get(name) != want {
					t.Errorf("incorrect token form parameter %s", name)
				}
			}
			clientID, secret, ok := r.BasicAuth()
			if !ok {
				clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
			}
			if clientID != "test-client" || secret != "test-secret" {
				t.Error("incorrect OAuth client authentication")
			}
			if tc.tokenError {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			claims := map[string]any{
				"iss": server.URL, "aud": "test-client", "sub": "verified-subject",
				"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
				"nonce": "expected-nonce", "email": "ada@example.com", "email_verified": true,
				"given_name": "Ada", "family_name": "Lovelace",
			}
			if tc.mutate != nil {
				tc.mutate(claims)
			}
			payload, err := json.Marshal(claims)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			response := map[string]any{
				"access_token": "unused-access-token", "refresh_token": "unused-refresh-token",
				"token_type": "Bearer", "expires_in": 3600,
			}
			if !tc.missingIDToken {
				response["id_token"] = raw
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	})
	if tc.tls {
		server = httptest.NewTLSServer(handler)
	} else {
		server = httptest.NewServer(handler)
	}
	t.Cleanup(server.Close)
	ctx := oidc.ClientContext(context.Background(), server.Client())
	provider, err := New(ctx, server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	if provider.client.Timeout != 10*time.Second || server.Client().Timeout != 0 {
		t.Fatal("provider must bound HTTP timeouts without mutating injected client")
	}
	return provider, server.URL, &tokenRequests
}

func TestProviderDiscoveryAuthorizationAndExchangeE2E(t *testing.T) {
	for _, tls := range []bool{false, true} {
		name := "HTTP"
		if tls {
			name = "HTTPS"
		}
		t.Run(name, func(t *testing.T) {
			provider, issuer, tokenRequests := mockProvider(t, oidcTestCase{tls: tls})
			authorization, err := url.Parse(provider.AuthorizationURL("expected-state", "expected-nonce", "expected-verifier"))
			if err != nil || authorization.Scheme+"://"+authorization.Host+authorization.Path != issuer+"/authorize" {
				t.Fatal("incorrect discovered authorization endpoint")
			}
			query := authorization.Query()
			challenge := sha256.Sum256([]byte("expected-verifier"))
			for name, want := range map[string]string{
				"state": "expected-state", "nonce": "expected-nonce", "scope": "openid email profile",
				"response_type": "code", "client_id": "test-client", "code_challenge_method": "S256",
				"code_challenge": base64.RawURLEncoding.EncodeToString(challenge[:]),
			} {
				if query.Get(name) != want {
					t.Fatalf("incorrect authorization parameter %s", name)
				}
			}
			if query.Has("access_type") || strings.Contains(authorization.String(), "test-secret") {
				t.Fatal("offline access requested or client secret leaked")
			}
			identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
			if err != nil {
				t.Fatal(err)
			}
			want := domain.GoogleIdentity{Subject: "verified-subject", Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"}
			if identity != want || tokenRequests.Load() != 1 {
				t.Fatalf("identity %+v token requests %d", identity, tokenRequests.Load())
			}
			t.Log("local OIDC discovery, token exchange, JWKS signature validation and verified identity passed")
		})
	}
}

func TestProviderRejectsInvalidIdentity(t *testing.T) {
	cases := map[string]oidcTestCase{
		"issuer":                     {mutate: func(claims map[string]any) { claims["iss"] = "https://other.example" }},
		"audience":                   {mutate: func(claims map[string]any) { claims["aud"] = "other-client" }},
		"expired":                    {mutate: func(claims map[string]any) { claims["exp"] = time.Now().Add(-time.Hour).Unix() }},
		"signature":                  {invalidSignature: true},
		"nonce":                      {mutate: func(claims map[string]any) { claims["nonce"] = "incorrect-nonce" }},
		"missing nonce":              {mutate: func(claims map[string]any) { delete(claims, "nonce") }},
		"unverified email":           {mutate: func(claims map[string]any) { claims["email_verified"] = false }},
		"missing email verification": {mutate: func(claims map[string]any) { delete(claims, "email_verified") }},
		"invalid verification type":  {mutate: func(claims map[string]any) { claims["email_verified"] = "true" }},
		"missing email":              {mutate: func(claims map[string]any) { delete(claims, "email") }},
		"missing subject":            {mutate: func(claims map[string]any) { delete(claims, "sub") }},
		"missing ID token":           {missingIDToken: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider, _, _ := mockProvider(t, tc)
			identity, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce")
			if !errors.Is(err, domain.ErrInvalidIdentity) || identity != (domain.GoogleIdentity{}) {
				t.Fatalf("invalid identity accepted: %+v error %v", identity, err)
			}
		})
	}
}

func TestProviderExchangeRejectsMissingFlowSecrets(t *testing.T) {
	provider, _, tokenRequests := mockProvider(t, oidcTestCase{})
	for _, values := range [][3]string{
		{"", "expected-verifier", "expected-nonce"},
		{"authorization-code", "", "expected-nonce"},
		{"authorization-code", "expected-verifier", ""},
	} {
		if _, err := provider.Exchange(context.Background(), values[0], values[1], values[2]); !errors.Is(err, domain.ErrInvalidIdentity) {
			t.Fatalf("missing flow value: error %v", err)
		}
	}
	if tokenRequests.Load() != 0 {
		t.Fatal("token exchange attempted without flow secrets")
	}
}

func TestProviderExchangeErrorsAndCancellation(t *testing.T) {
	provider, _, _ := mockProvider(t, oidcTestCase{tokenError: true})
	if _, err := provider.Exchange(context.Background(), "authorization-code", "expected-verifier", "expected-nonce"); err == nil {
		t.Fatal("token endpoint error ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Exchange(ctx, "authorization-code", "expected-verifier", "expected-nonce"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestProviderDiscoveryRejectsWrongIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "https://other.example"})
	}))
	defer server.Close()
	if _, err := New(context.Background(), server.URL, "test-client", "test-secret", "http://localhost:8080/api/auth/google/callback"); err == nil {
		t.Fatal("mismatched discovery issuer accepted")
	}
}

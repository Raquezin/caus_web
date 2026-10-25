package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type authLogSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *authLogSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *authLogSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type authTestClaims struct {
	Values           map[string]any
	InvalidSignature bool
}

func identityClaims(identity domain.GoogleIdentity) authTestClaims {
	return authTestClaims{Values: map[string]any{
		"sub": identity.Subject, "email": identity.Email,
		"given_name": identity.FirstName, "family_name": identity.LastName,
	}}
}

type mockAuthorization struct {
	nonce       string
	challenge   string
	redirectURI string
	claims      authTestClaims
}

type authOIDCMock struct {
	Server        *httptest.Server
	TokenRequests atomic.Int32
	KeysDown      atomic.Bool
	mu            sync.Mutex
	nextClaims    authTestClaims
	codes         map[string]mockAuthorization
	signer        jose.Signer
	badSigner     jose.Signer
	key           *rsa.PublicKey
}

func newAuthOIDCMock(t *testing.T) *authOIDCMock {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	badKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	makeSigner := func(key *rsa.PrivateKey) jose.Signer {
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "auth-e2e"))
		if err != nil {
			t.Fatal(err)
		}
		return signer
	}
	mock := &authOIDCMock{
		codes: make(map[string]mockAuthorization), signer: makeSigner(key), badSigner: makeSigner(badKey), key: &key.PublicKey,
	}
	mock.Server = httptest.NewServer(http.HandlerFunc(mock.serveHTTP))
	t.Cleanup(mock.Server.Close)
	return mock
}

func (m *authOIDCMock) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer": m.Server.URL, "authorization_endpoint": m.Server.URL + "/authorize",
			"token_endpoint": m.Server.URL + "/token", "jwks_uri": m.Server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	case "/keys":
		if m.KeysDown.Load() {
			writeError(w, http.StatusServiceUnavailable, "internal_error", "internal error")
			return
		}
		writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: m.key, KeyID: "auth-e2e", Use: "sig", Algorithm: string(jose.RS256)},
		}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != "test-client" || q.Get("response_type") != "code" ||
			q.Get("scope") != "openid email profile" || q.Get("code_challenge_method") != "S256" ||
			!validOAuthRandom(q.Get("code_challenge")) || !validOAuthRandom(q.Get("nonce")) || !validOAuthRandom(q.Get("state")) {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid authorization")
			return
		}
		code := uuid.NewString()
		m.mu.Lock()
		m.codes[code] = mockAuthorization{
			nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri"), claims: m.nextClaims,
		}
		m.mu.Unlock()
		redirect, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid redirect")
			return
		}
		query := redirect.Query()
		query.Set("state", q.Get("state"))
		query.Set("code", code)
		redirect.RawQuery = query.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	case "/token":
		m.TokenRequests.Add(1)
		if r.Method != http.MethodPost || r.ParseForm() != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		m.mu.Lock()
		authorization, exists := m.codes[r.PostForm.Get("code")]
		delete(m.codes, r.PostForm.Get("code"))
		m.mu.Unlock()
		challenge := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !exists || id != "test-client" || secret != "test-secret" ||
			r.PostForm.Get("grant_type") != "authorization_code" ||
			r.PostForm.Get("redirect_uri") != authorization.redirectURI ||
			base64.RawURLEncoding.EncodeToString(challenge[:]) != authorization.challenge {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		claims := map[string]any{
			"iss": m.Server.URL, "aud": "test-client", "sub": "verified-subject",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
			"nonce": authorization.nonce, "email": "ada@example.com", "email_verified": true,
			"given_name": "Ada", "family_name": "Lovelace",
		}
		for key, value := range authorization.claims.Values {
			claims[key] = value
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
		signer := m.signer
		if authorization.claims.InvalidSignature {
			signer = m.badSigner
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
		raw, err := signed.CompactSerialize()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "access-marker", "refresh_token": "refresh-marker",
			"token_type": "Bearer", "expires_in": 3600, "id_token": raw,
		})
	default:
		http.NotFound(w, r)
	}
}

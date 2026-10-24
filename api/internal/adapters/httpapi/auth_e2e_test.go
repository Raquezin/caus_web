package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/adapters/google"
	"caus-platform/api/internal/adapters/postgres"
	"caus-platform/api/internal/application"
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

type authE2EFixture struct {
	Server *httptest.Server
	Client *http.Client
	Tx     pgx.Tx
	Auth   *application.Auth
	Logs   *authLogSink
	OIDC   *authOIDCMock
	TTL    time.Duration
}

func newAuthE2EFixture(t *testing.T, adminEmails ...string) *authE2EFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping auth PostgreSQL/OIDC integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	schema := "auth_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"functions.sql", "people.sql", "auth.sql"} {
		sql, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schema", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	f := &authE2EFixture{Tx: tx, Logs: &authLogSink{}, OIDC: newAuthOIDCMock(t), TTL: time.Hour}
	f.Auth = application.NewAuth(postgres.NewAuthRepository(tx), f.TTL, adminEmails)
	f.Server = httptest.NewUnstartedServer(nil)
	t.Cleanup(f.Server.Close)
	_, port, err := net.SplitHostPort(f.Server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	publicURL := "http://localhost:" + port
	provider, err := google.New(ctx, f.OIDC.Server.URL, "test-client", "test-secret", publicURL+"/api/auth/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(f.Logs, nil))
	auth := NewAuth(provider, f.Auth, AuthConfig{PublicURL: publicURL, SessionTTL: f.TTL}, log)
	f.Server.Config.Handler = NewRouter(application.NewPeople(postgres.NewPeopleRepository(tx)), pool.Ping, log, auth)
	f.Server.Start()
	f.Server.URL = publicURL
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.Client = &http.Client{
		Jar: jar, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return f
}

type pendingAuthLogin struct {
	CallbackURL string
	Cookie      *http.Cookie
	Flow        oauthFlow
}

type authLoginResult struct {
	Response *http.Response
	Raw      string
	Hash     [32]byte
	Pending  pendingAuthLogin
}

func (f *authE2EFixture) BeginLogin(t *testing.T, claims authTestClaims) pendingAuthLogin {
	t.Helper()
	f.OIDC.mu.Lock()
	f.OIDC.nextClaims = claims
	f.OIDC.mu.Unlock()
	response, err := f.Client.Get(f.Server.URL + "/api/auth/google/login?return_to=%2Fcuenta%2F%3Ffrom%3Dlogin")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("login status %d", response.StatusCode)
	}
	cookie, flow := loginFlow(t, response)
	authorize, err := f.Client.Get(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	defer authorize.Body.Close()
	if authorize.StatusCode != http.StatusFound {
		t.Fatalf("authorization status %d", authorize.StatusCode)
	}
	return pendingAuthLogin{CallbackURL: authorize.Header.Get("Location"), Cookie: cookie, Flow: flow}
}

func (f *authE2EFixture) Login(t *testing.T, claims authTestClaims) authLoginResult {
	t.Helper()
	pending := f.BeginLogin(t, claims)
	response, err := f.Client.Get(pending.CallbackURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	result := authLoginResult{Response: response, Pending: pending}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "caus_session" {
			result.Raw = cookie.Value
			result.Hash = sha256.Sum256([]byte(cookie.Value))
		}
	}
	return result
}

func (f *authE2EFixture) counts(t *testing.T) (int, int) {
	t.Helper()
	var people, sessions int
	if err := f.Tx.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM people), (SELECT count(*) FROM sessions)").Scan(&people, &sessions); err != nil {
		t.Fatal(err)
	}
	return people, sessions
}

func assertAuthError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil || response.StatusCode != status || body.Error.Code != code {
		t.Fatalf("status %d, code %q; wanted %d %q", response.StatusCode, body.Error.Code, status, code)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "caus_session" {
			t.Fatal("callback failure set a session cookie")
		}
	}
	assertOAuthCleared(t, response)
}

func assertOAuthCleared(t *testing.T, response *http.Response) {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == "caus_oauth" {
			if cookie.Value != "" || cookie.MaxAge != -1 || !cookie.Expires.Before(time.Now()) ||
				cookie.Path != "/api/auth/google" || cookie.Domain != "" || !cookie.HttpOnly ||
				cookie.SameSite != http.SameSiteLaxMode || cookie.Secure {
				t.Fatal("incorrect OAuth deletion cookie")
			}
			return
		}
	}
	t.Fatal("OAuth flow cookie not deleted")
}

func TestGoogleCallbackOIDCPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	before := time.Now()
	result := f.Login(t, identityClaims(domain.GoogleIdentity{
		Subject: "verified-subject", Email: "  Ada@Example.COM  ", FirstName: "Ada", LastName: "Lovelace",
	}))
	if result.Response.StatusCode != http.StatusFound || result.Response.Header.Get("Location") != "/cuenta/?from=login" || len(result.Raw) != 43 {
		t.Fatal("callback failed to redirect with session")
	}
	assertOAuthCleared(t, result.Response)
	var id uuid.UUID
	var first, last, email, sub, role string
	var hash []byte
	var expires, created time.Time
	err := f.Tx.QueryRow(context.Background(),
		"SELECT p.id, p.first_name, p.last_name, p.email, p.google_sub, p.role, s.token_hash, s.expires_at, s.created_at FROM people p JOIN sessions s ON p.id = s.person_id").
		Scan(&id, &first, &last, &email, &sub, &role, &hash, &expires, &created)
	if err != nil {
		t.Fatal(err)
	}
	if first != "Ada" || last != "Lovelace" || email != "ada@example.com" || sub != "verified-subject" || role != "member" ||
		len(hash) != 32 || !bytes.Equal(hash, result.Hash[:]) || bytes.Equal(hash, []byte(result.Raw)) ||
		!expires.After(created) || expires.Before(before.Add(f.TTL)) || expires.After(time.Now().Add(f.TTL)) {
		t.Fatal("profile, role, token hashing or expiry differs from schema/auth contract")
	}
	var session *http.Cookie
	for _, cookie := range result.Response.Cookies() {
		if cookie.Name == "caus_session" {
			session = cookie
		}
	}
	if session == nil || session.Path != "/" || session.Domain != "" || !session.HttpOnly || session.Secure ||
		session.SameSite != http.SameSiteLaxMode || session.MaxAge != int(f.TTL/time.Second) ||
		session.Expires.Before(before.Add(f.TTL).Truncate(time.Second)) || session.Expires.After(time.Now().Add(f.TTL)) {
		t.Fatal("incorrect session cookie")
	}
	account, err := f.Auth.CurrentUser(context.Background(), result.Raw)
	if err != nil || account.ID != id || account.Email != email {
		t.Fatal("session cannot load joined account")
	}
	second := f.Login(t, authTestClaims{Values: map[string]any{"email": "new@example.com", "given_name": "Augusta"}})
	if second.Response.StatusCode != http.StatusFound || second.Raw == result.Raw {
		t.Fatal("returning subject did not get a fresh session")
	}
	account, err = f.Auth.CurrentUser(context.Background(), second.Raw)
	if err != nil || account.ID != id || account.Email != "new@example.com" || account.FirstName != "Augusta" {
		t.Fatal("subject reuse failed to update the verified profile in place")
	}
	if people, sessions := f.counts(t); people != 1 || sessions != 2 {
		t.Fatal("returning subject duplicated person or lost session")
	}
	if err := f.Auth.Logout(context.Background(), result.Raw); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Auth.CurrentUser(context.Background(), result.Raw); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted session remained valid")
	}
	data, err := io.ReadAll(result.Response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{result.Raw, result.Pending.Flow.State, result.Pending.Flow.Nonce, result.Pending.Flow.Verifier,
		"ada@example.com", "access-marker", "refresh-marker", "test-secret", "new@example.com",
		urlQuery(t, result.Pending.CallbackURL).Get("code")} {
		if strings.Contains(f.Logs.String(), secret) || strings.Contains(string(data), secret) {
			t.Fatal("callback response or logs leaked a credential or claim")
		}
	}
	t.Log("real HTTP login/authorization/code/PKCE/JWKS callback and rollback-isolated PostgreSQL session passed")
}

func urlQuery(t *testing.T, value string) url.Values {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query()
}

func TestGoogleCallbackLinkingAndRolesE2E(t *testing.T) {
	cases := []struct {
		name     string
		subject  any
		role     string
		admins   []string
		wantRole string
		status   int
	}{
		{name: "unlinked email", role: "member", wantRole: "member", status: 302},
		{name: "bootstrap promotion", role: "member", admins: []string{" ADA@EXAMPLE.COM "}, wantRole: "admin", status: 302},
		{name: "preserve admin", subject: "verified-subject", role: "admin", wantRole: "admin", status: 302},
		{name: "linked collision", subject: "another-subject", role: "member", status: 409},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthE2EFixture(t, tc.admins...)
			id := uuid.New()
			if _, err := f.Tx.Exec(context.Background(),
				"INSERT INTO people (id, first_name, last_name, email, google_sub, role) VALUES ($1, 'Original', 'Person', 'ada@example.com', $2, $3)",
				id, tc.subject, tc.role); err != nil {
				t.Fatal(err)
			}
			result := f.Login(t, authTestClaims{})
			if tc.status == 409 {
				assertAuthError(t, result.Response, 409, "identity_conflict")
				var first, subject string
				if err := f.Tx.QueryRow(context.Background(), "SELECT first_name, google_sub FROM people WHERE id = $1", id).Scan(&first, &subject); err != nil {
					t.Fatal(err)
				}
				if first != "Original" || subject != tc.subject || result.Raw != "" {
					t.Fatal("collision mutated linked account")
				}
				if people, sessions := f.counts(t); people != 1 || sessions != 0 {
					t.Fatal("collision created account or session")
				}
				return
			}
			account, err := f.Auth.CurrentUser(context.Background(), result.Raw)
			if err != nil || result.Response.StatusCode != tc.status || account.ID != id || account.Role != tc.wantRole || account.FirstName != "Ada" {
				t.Fatal("verified linking or role preservation failed")
			}
		})
	}
}

func TestGoogleCallbackInvalidClaimsE2E(t *testing.T) {
	cases := map[string]struct {
		claims authTestClaims
		status int
	}{
		"nonce":       {claims: authTestClaims{Values: map[string]any{"nonce": "wrong"}}, status: 401},
		"unverified":  {claims: authTestClaims{Values: map[string]any{"email_verified": false}}, status: 401},
		"issuer":      {claims: authTestClaims{Values: map[string]any{"iss": "https://other.example"}}, status: 401},
		"audience":    {claims: authTestClaims{Values: map[string]any{"aud": "other-client"}}, status: 401},
		"signature":   {claims: authTestClaims{InvalidSignature: true}, status: 401},
		"expired":     {claims: authTestClaims{Values: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}}, status: 401},
		"first name":  {claims: authTestClaims{Values: map[string]any{"given_name": ""}}, status: 422},
		"last name":   {claims: authTestClaims{Values: map[string]any{"family_name": " "}}, status: 422},
		"email form":  {claims: authTestClaims{Values: map[string]any{"email": "Ada <ada@example.com>"}}, status: 422},
		"schema mail": {claims: authTestClaims{Values: map[string]any{"email": "ada@localhost"}}, status: 422},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAuthE2EFixture(t)
			result := f.Login(t, tc.claims)
			assertAuthError(t, result.Response, tc.status, "invalid_identity")
			if people, sessions := f.counts(t); people != 0 || sessions != 0 || f.OIDC.TokenRequests.Load() != 1 {
				t.Fatal("invalid verified identity mutated database or skipped real exchange")
			}
		})
	}
}

func TestGoogleCallbackRejectsFlowBeforeExchangeE2E(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*pendingAuthLogin)
		code   string
	}{
		{name: "wrong state", mutate: func(p *pendingAuthLogin) { p.CallbackURL += "&state=wrong" }, code: "oauth_callback_invalid"},
		{name: "mismatch", mutate: func(p *pendingAuthLogin) { p.CallbackURL = strings.Replace(p.CallbackURL, p.Flow.State, "wrong", 1) }, code: "invalid_oauth_state"},
		{name: "missing cookie", mutate: func(p *pendingAuthLogin) { p.Cookie = nil }, code: "invalid_oauth_state"},
		{name: "bad cookie", mutate: func(p *pendingAuthLogin) { p.Cookie.Value = "invalid" }, code: "invalid_oauth_state"},
		{name: "expired", mutate: func(p *pendingAuthLogin) {
			p.Flow.ExpiresAt = time.Now().Add(-time.Second).Unix()
			replaceFlowCookie(p)
		}, code: "invalid_oauth_state"},
		{name: "unsafe return", mutate: func(p *pendingAuthLogin) {
			p.Flow.ReturnTo = "//evil.example"
			replaceFlowCookie(p)
		}, code: "invalid_oauth_state"},
		{name: "duplicate code", mutate: func(p *pendingAuthLogin) { p.CallbackURL += "&code=other" }, code: "oauth_callback_invalid"},
		{name: "malformed", mutate: func(p *pendingAuthLogin) { p.CallbackURL += "&bad=%ZZ" }, code: "oauth_callback_invalid"},
		{name: "denied", mutate: func(p *pendingAuthLogin) { replaceCallbackCode(p, "access_denied") }, code: "oauth_denied"},
		{name: "provider error", mutate: func(p *pendingAuthLogin) { replaceCallbackCode(p, "private-provider-detail") }, code: "oauth_callback_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthE2EFixture(t)
			pending := f.BeginLogin(t, authTestClaims{})
			tc.mutate(&pending)
			request, err := http.NewRequest(http.MethodGet, pending.CallbackURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if pending.Cookie != nil {
				request.AddCookie(pending.Cookie)
			}
			client := *f.Client
			client.Jar = nil
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			assertAuthError(t, response, 400, tc.code)
			if people, sessions := f.counts(t); people != 0 || sessions != 0 || f.OIDC.TokenRequests.Load() != 0 {
				t.Fatal("invalid callback exchanged a code or mutated DB")
			}
			if strings.Contains(f.Logs.String(), "private-provider-detail") {
				t.Fatal("provider error leaked into logs")
			}
		})
	}
}

func replaceFlowCookie(p *pendingAuthLogin) {
	data, _ := json.Marshal(p.Flow)
	p.Cookie.Value = base64.RawURLEncoding.EncodeToString(data)
}

func replaceCallbackCode(p *pendingAuthLogin, providerError string) {
	u, _ := url.Parse(p.CallbackURL)
	q := u.Query()
	q.Del("code")
	q.Set("error", providerError)
	q.Set("error_description", "private-provider-detail")
	u.RawQuery = q.Encode()
	p.CallbackURL = u.String()
}

func TestGoogleCallbackCodeIsSingleUseE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != 302 {
		t.Fatal("first callback failed")
	}
	request, err := http.NewRequest(http.MethodGet, result.Pending.CallbackURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(result.Pending.Cookie)
	client := *f.Client
	client.Jar = nil
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	assertAuthError(t, response, 500, "internal_error")
	if people, sessions := f.counts(t); people != 1 || sessions != 1 {
		t.Fatal("replayed code created another session")
	}
	if f.OIDC.TokenRequests.Load() < 2 {
		t.Fatal("single-use code was not enforced by real token endpoint")
	}
}

func TestGoogleCallbackHEADPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t, "ada@example.com")
	pending := f.BeginLogin(t, authTestClaims{})
	request, err := http.NewRequest(http.MethodHead, pending.CallbackURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := f.Client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 0 || response.StatusCode != 302 || response.Header.Get("Location") != pending.Flow.ReturnTo {
		t.Fatal("real HEAD callback did not redirect with empty body")
	}
	assertOAuthCleared(t, response)
	var raw string
	for _, cookie := range response.Cookies() {
		if cookie.Name == "caus_session" {
			raw = cookie.Value
		}
	}
	account, err := f.Auth.CurrentUser(context.Background(), raw)
	if err != nil || account.Role != "admin" {
		t.Fatal("HEAD callback failed to persist bootstrap-admin session")
	}
	if people, sessions := f.counts(t); people != 1 || sessions != 1 || f.OIDC.TokenRequests.Load() != 1 {
		t.Fatal("HEAD callback did not use real OIDC and PostgreSQL")
	}
}

func TestGoogleCallbackDatabaseFailureAtomicityE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	if _, err := f.Tx.Exec(context.Background(), "ALTER TABLE sessions ADD CONSTRAINT reject_test_session CHECK (false)"); err != nil {
		t.Fatal(err)
	}
	result := f.Login(t, authTestClaims{})
	assertAuthError(t, result.Response, 500, "internal_error")
	if people, sessions := f.counts(t); people != 0 || sessions != 0 {
		t.Fatal("failed session insertion left partial person changes")
	}
	for _, value := range []string{"reject_test_session", "ada@example.com", "verified-subject", "test-secret", "access-marker", "refresh-marker"} {
		if strings.Contains(f.Logs.String(), value) {
			t.Fatal("database failure log leaked private data")
		}
	}
	if !strings.Contains(f.Logs.String(), "error_type") {
		t.Fatal("sanitized error type not logged")
	}
	t.Log(fmt.Sprintf("atomic callback failure verified after %d real exchange", f.OIDC.TokenRequests.Load()))
}

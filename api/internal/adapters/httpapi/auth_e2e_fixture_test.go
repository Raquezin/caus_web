package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/adapters/google"
	"caus-platform/api/internal/adapters/postgres"
	"caus-platform/api/internal/application"
)

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
	for _, name := range []string{"functions.sql", "people.sql", "auth.sql", "admin_email_bindings.sql"} {
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
	provider := google.New(ctx, f.OIDC.Server.URL, "test-client", "test-secret", publicURL+"/api/auth/google/callback")
	log := slog.New(slog.NewJSONHandler(f.Logs, nil))
	auth := NewAuth(provider, f.Auth, AuthConfig{PublicURL: publicURL, SessionTTL: f.TTL}, log)
	f.Server.Config.Handler = NewRouterWithBackend(application.NewPeople(postgres.NewPeopleRepository(tx)), pool.Ping, log, auth, NewBackend(nil, auth, log))
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

func assertSignInRedirect(t *testing.T, response *http.Response, returnTo, code string) {
	t.Helper()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("sign-in rejection: status %d, wanted 302", response.StatusCode)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("unparsable redirect %q: %v", response.Header.Get("Location"), err)
	}
	expected, err := url.Parse(returnTo)
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != expected.Path || location.Fragment != expected.Fragment {
		t.Fatalf("redirect %q does not keep return_to %q", location, expected)
	}
	params := location.Query()
	if params.Get("auth_error") != code || len(params["auth_error"]) != 1 {
		t.Fatalf("redirect %q missing auth_error=%q", location, code)
	}
	params.Del("auth_error")
	if got, want := params.Encode(), expected.Query().Encode(); got != want {
		t.Fatalf("redirect %q lost return_to parameters %q", location, want)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "caus_session" && cookie.Value != "" {
			t.Fatal("sign-in failure set a session cookie")
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

func urlQuery(t *testing.T, value string) url.Values {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query()
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

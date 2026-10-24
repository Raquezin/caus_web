package httpapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

func TestLogoutOIDCPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	first := f.Login(t, authTestClaims{})
	account, err := f.Auth.CurrentUser(context.Background(), first.Raw)
	if first.Response.StatusCode != 302 || err != nil {
		t.Fatal("real OIDC login did not create a session")
	}
	second := f.Login(t, authTestClaims{})
	other := f.Login(t, authTestClaims{Values: map[string]any{
		"sub": "grace-subject", "email": "grace@example.com", "given_name": "Grace", "family_name": "Hopper",
	}})
	if second.Response.StatusCode != 302 || other.Response.StatusCode != 302 {
		t.Fatal("additional OIDC logins failed")
	}
	serverURL, err := url.Parse(f.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	f.Client.Jar.SetCookies(serverURL, first.Response.Cookies())
	request, err := http.NewRequest(http.MethodPost, f.Server.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", f.Server.URL)
	response, err := f.Client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 204 || len(body) != 0 {
		t.Fatal("logout did not return an empty 204")
	}
	assertLogoutCookie(t, response, false)
	var exists bool
	if err := f.Tx.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = $1)", first.Hash[:]).Scan(&exists); err != nil || exists {
		t.Fatal("logout did not delete exactly the presented token hash")
	}
	if _, err := f.Auth.CurrentUser(context.Background(), first.Raw); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted session remained authenticated")
	}
	retained, err := f.Auth.CurrentUser(context.Background(), second.Raw)
	if err != nil || retained != account {
		t.Fatal("logout changed the person or revoked another session for the same person")
	}
	otherAccount, err := f.Auth.CurrentUser(context.Background(), other.Raw)
	if err != nil || otherAccount.ID == account.ID || otherAccount.Email != "grace@example.com" {
		t.Fatal("logout affected another person's session")
	}
	if people, sessions := f.counts(t); people != 2 || sessions != 2 {
		t.Fatal("logout deleted a person or unrelated session")
	}
	for _, cookie := range f.Client.Jar.Cookies(serverURL) {
		if cookie.Name == "caus_session" {
			t.Fatal("cookiejar retained a cached valid session")
		}
	}
	repeat, err := http.NewRequest(http.MethodPost, f.Server.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	repeat.Header.Set("Origin", f.Server.URL)
	repeated, err := f.Client.Do(repeat)
	if err != nil {
		t.Fatal(err)
	}
	defer repeated.Body.Close()
	assertLogoutError(t, repeated, 401, "unauthenticated")
	assertLogoutCookie(t, repeated, false)
	repeat.AddCookie(&http.Cookie{Name: "caus_session", Value: first.Raw})
	replayed, err := f.Client.Do(repeat)
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Body.Close()
	assertLogoutError(t, replayed, 401, "unauthenticated")
	assertLogoutCookie(t, replayed, false)
	if people, sessions := f.counts(t); people != 2 || sessions != 2 {
		t.Fatal("repeated logout changed unrelated data")
	}
	for _, secret := range []string{first.Raw, second.Raw, other.Raw, "ada@example.com", "grace@example.com"} {
		if strings.Contains(f.Logs.String(), secret) {
			t.Fatal("logout logged a token or personal data")
		}
	}
	t.Log("real OIDC/PostgreSQL logout removed only the presented session, retained both people and unrelated sessions, and deleted the browser cookie")
}

func TestLogoutOriginsPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != 302 {
		t.Fatal("OIDC login failed")
	}
	serverURL, err := url.Parse(f.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	otherPort := "8080"
	if serverURL.Port() == otherPort {
		otherPort = "8081"
	}
	cases := []struct {
		name    string
		origins []string
		code    string
	}{
		{name: "missing", code: "origin_denied"},
		{name: "empty", origins: []string{""}, code: "origin_denied"},
		{name: "foreign", origins: []string{"https://foreign.example"}, code: "cors_origin_denied"},
		{name: "other allowed localhost port", origins: []string{"http://" + net.JoinHostPort("localhost", otherPort)}, code: "origin_denied"},
		{name: "allowed production origin", origins: []string{allowedOrigin}, code: "origin_denied"},
		{name: "null", origins: []string{"null"}, code: "cors_origin_denied"},
		{name: "duplicate", origins: []string{f.Server.URL, f.Server.URL}, code: "origin_denied"},
		{name: "duplicate foreign", origins: []string{f.Server.URL, "https://foreign.example"}, code: "origin_denied"},
		{name: "duplicate foreign first", origins: []string{"https://foreign.example", f.Server.URL}, code: "cors_origin_denied"},
		{name: "malformed", origins: []string{"http://[invalid"}, code: "cors_origin_denied"},
		{name: "comma joined", origins: []string{f.Server.URL + ", " + f.Server.URL}, code: "cors_origin_denied"},
		{name: "path", origins: []string{f.Server.URL + "/logout"}, code: "cors_origin_denied"},
	}
	for _, tc := range cases {
		for _, authenticated := range []bool{false, true} {
			name := tc.name + "/without-session"
			if authenticated {
				name = tc.name + "/with-session"
			}
			t.Run(name, func(t *testing.T) {
				request, err := http.NewRequest(http.MethodPost, f.Server.URL+"/api/auth/logout", nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, origin := range tc.origins {
					request.Header.Add("Origin", origin)
				}
				client := *f.Client
				client.Jar = nil
				if authenticated {
					request.AddCookie(&http.Cookie{Name: "caus_session", Value: result.Raw})
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				assertLogoutError(t, response, 403, tc.code)
				if len(response.Cookies()) != 0 {
					t.Fatal("Origin rejection mutated cookies")
				}
				if _, err := f.Auth.CurrentUser(context.Background(), result.Raw); err != nil {
					t.Fatal("Origin rejection invalidated the session")
				}
				if people, sessions := f.counts(t); people != 1 || sessions != 1 {
					t.Fatal("Origin rejection mutated auth data")
				}
			})
		}
	}
}

func TestLogoutInvalidSessionsPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	valid := f.Login(t, authTestClaims{})
	expired := f.Login(t, authTestClaims{})
	if valid.Response.StatusCode != 302 || expired.Response.StatusCode != 302 {
		t.Fatal("OIDC logins failed")
	}
	if _, err := f.Tx.Exec(context.Background(),
		"UPDATE sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE token_hash = $1", expired.Hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Auth.CurrentUser(context.Background(), expired.Raw); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("expired fixture session was not rejected")
	}
	cases := []struct {
		name   string
		cookie string
	}{
		{name: "missing"},
		{name: "empty", cookie: "caus_session="},
		{name: "malformed short", cookie: "caus_session=invalid"},
		{name: "malformed alphabet", cookie: "caus_session=" + strings.Repeat("!", 43)},
		{name: "noncanonical encoding", cookie: "caus_session=" + strings.Repeat("A", 42) + "B"},
		{name: "nonexistent", cookie: "caus_session=" + oauth2.GenerateVerifier()},
		{name: "expired", cookie: "caus_session=" + expired.Raw},
		{name: "duplicate", cookie: "caus_session=" + valid.Raw + "; caus_session=" + valid.Raw},
		{name: "duplicate other session", cookie: "caus_session=" + valid.Raw + "; caus_session=" + expired.Raw},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, f.Server.URL+"/api/auth/logout", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Origin", f.Server.URL)
			if tc.cookie != "" {
				request.Header.Set("Cookie", tc.cookie)
			}
			client := *f.Client
			client.Jar = nil
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			assertLogoutError(t, response, 401, "unauthenticated")
			assertLogoutCookie(t, response, false)
			if _, err := f.Auth.CurrentUser(context.Background(), valid.Raw); err != nil {
				t.Fatal("invalid or ambiguous cookie revoked a valid session")
			}
			if people, sessions := f.counts(t); people != 1 || sessions != 2 {
				t.Fatal("unauthenticated logout changed people or sessions")
			}
		})
	}
}

func TestLogoutMethodsPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != 302 {
		t.Fatal("OIDC login failed")
	}
	cases := []struct {
		method    string
		preflight string
		status    int
		allow     string
	}{
		{method: http.MethodGet, status: 405, allow: "POST"},
		{method: http.MethodHead, status: 405, allow: "POST"},
		{method: http.MethodPut, status: 405, allow: "POST"},
		{method: http.MethodPatch, status: 405, allow: "POST"},
		{method: http.MethodDelete, status: 405, allow: "POST"},
		{method: http.MethodOptions, status: 405, allow: "POST"},
		{method: http.MethodOptions, preflight: http.MethodPost, status: 405, allow: "GET, HEAD, OPTIONS"},
		{method: http.MethodOptions, preflight: http.MethodGet, status: 204},
	}
	for _, tc := range cases {
		t.Run(tc.method+"/"+tc.preflight, func(t *testing.T) {
			request, err := http.NewRequest(tc.method, f.Server.URL+"/api/auth/logout", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Origin", f.Server.URL)
			if tc.preflight != "" {
				request.Header.Set("Access-Control-Request-Method", tc.preflight)
			}
			response, err := f.Client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status || response.Header.Get("Allow") != tc.allow || len(response.Cookies()) != 0 {
				t.Fatal("method guard or existing CORS preflight contract changed")
			}
			if tc.method == http.MethodHead || tc.status == 204 {
				body, err := io.ReadAll(response.Body)
				if err != nil || len(body) != 0 {
					t.Fatal("HEAD/preflight returned a body")
				}
			} else {
				assertLogoutError(t, response, 405, "method_not_allowed")
			}
			if _, err := f.Auth.CurrentUser(context.Background(), result.Raw); err != nil {
				t.Fatal("unsupported method logged out the session")
			}
			if people, sessions := f.counts(t); people != 1 || sessions != 1 {
				t.Fatal("unsupported method mutated auth data")
			}
		})
	}
}

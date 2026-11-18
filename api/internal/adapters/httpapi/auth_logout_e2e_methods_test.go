package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

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
		{method: http.MethodOptions, preflight: http.MethodPost, status: 204},
		{method: http.MethodOptions, preflight: http.MethodGet, status: 405, allow: "OPTIONS, POST"},
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

func TestGoogleLoginMeLogoutE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	assertMeError(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"),
		http.StatusUnauthorized, "unauthenticated", "authentication required")
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != http.StatusFound || len(result.Raw) != 43 {
		t.Fatal("OIDC callback did not establish session")
	}
	account := meDatabaseAccount(t, f)
	assertMeAccount(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"), account)
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
	if err != nil || response.StatusCode != http.StatusNoContent || len(body) != 0 {
		t.Fatal("logout did not return an empty 204")
	}
	assertLogoutCookie(t, response, false)
	assertMeError(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"),
		http.StatusUnauthorized, "unauthenticated", "authentication required")
	client := *f.Client
	client.Jar = nil
	assertMeError(t, meRequest(t, &client, http.MethodGet, f.Server.URL+"/api/me", "caus_session="+result.Raw),
		http.StatusUnauthorized, "unauthenticated", "authentication required")
	if people, sessions := f.counts(t); people != 1 || sessions != 0 || f.OIDC.TokenRequests.Load() != 1 {
		t.Fatal("login->me->logout flow left rows behind or made extra Google calls")
	}
	for _, secret := range []string{result.Raw, "ada@example.com", "verified-subject", result.Pending.Cookie.Value} {
		if strings.Contains(f.Logs.String(), secret) {
			t.Fatal("full auth flow leaked credentials or personal data to logs")
		}
	}
	t.Log("real HTTP OIDC login -> 200 /api/me -> 204 logout -> 401 /api/me; replayed raw cookie rejected, person retained, session revoked")
}

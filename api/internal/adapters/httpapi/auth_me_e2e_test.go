package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func meDatabaseAccount(t *testing.T, f *authE2EFixture) domain.Account {
	t.Helper()
	var account domain.Account
	if err := f.Tx.QueryRow(context.Background(),
		"SELECT id, first_name, last_name, email, role, created_at, updated_at FROM people").
		Scan(&account.ID, &account.FirstName, &account.LastName, &account.Email, &account.Role, &account.CreatedAt, &account.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return account
}

func meDatabaseSnapshot(t *testing.T, f *authE2EFixture) string {
	t.Helper()
	var snapshot string
	if err := f.Tx.QueryRow(context.Background(),
		"SELECT jsonb_build_object('people', (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM people p), 'sessions', (SELECT jsonb_agg(to_jsonb(s) ORDER BY s.token_hash) FROM sessions s))::text").
		Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestMeOIDCPostgresE2E(t *testing.T) {
	for _, role := range []string{"member", "admin"} {
		t.Run(role, func(t *testing.T) {
			var admins []string
			if role == "admin" {
				admins = []string{"ada@example.com"}
			}
			f := newAuthE2EFixture(t, admins...)
			result := f.Login(t, authTestClaims{})
			if result.Response.StatusCode != http.StatusFound || len(result.Raw) != 43 {
				t.Fatal("OIDC callback did not establish session")
			}
			account := meDatabaseAccount(t, f)
			if account.Role != role || account.Email != "ada@example.com" || account.FirstName != "Ada" || account.LastName != "Lovelace" {
				t.Fatal("unexpected verified account")
			}
			before := meDatabaseSnapshot(t, f)
			for range 2 {
				assertMeAccount(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"), account)
			}
			assertMeHead(t, meRequest(t, f.Client, http.MethodHead, f.Server.URL+"/api/me"), http.StatusOK)
			if before != meDatabaseSnapshot(t, f) {
				t.Fatal("current user reads mutated account or session")
			}
			if people, sessions := f.counts(t); people != 1 || sessions != 1 || f.OIDC.TokenRequests.Load() != 1 {
				t.Fatal("current user reads created rows or called Google")
			}
			for _, secret := range []string{
				result.Raw, base64.RawURLEncoding.EncodeToString(result.Hash[:]),
				"ada@example.com", "verified-subject", result.Pending.Cookie.Value,
			} {
				if strings.Contains(f.Logs.String(), secret) {
					t.Fatal("current user logs leaked credentials or private identity")
				}
			}
			t.Log("local OIDC login -> PostgreSQL hashed session -> GET/HEAD /api/me: exact seven-field UTC account, no cookie/DB mutations or further Google calls")
		})
	}
}

func TestMeInvalidSessionsE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != http.StatusFound {
		t.Fatal("OIDC callback did not establish session")
	}
	client := *f.Client
	client.Jar = nil
	cases := []struct {
		name    string
		cookies []string
	}{
		{name: "missing"},
		{name: "empty", cookies: []string{"caus_session="}},
		{name: "quoted empty", cookies: []string{`caus_session=""`}},
		{name: "bad", cookies: []string{"caus_session=not-a-session"}},
		{name: "invalid alphabet", cookies: []string{"caus_session=" + strings.Repeat("!", 43)}},
		{name: "too short", cookies: []string{"caus_session=" + strings.Repeat("A", 42)}},
		{name: "too long", cookies: []string{"caus_session=" + strings.Repeat("A", 44)}},
		{name: "padded", cookies: []string{"caus_session=" + result.Raw + "="}},
		{name: "noncanonical", cookies: []string{"caus_session=" + strings.Repeat("A", 42) + "B"}},
		{name: "nonexistent", cookies: []string{"caus_session=" + strings.Repeat("A", 43)}},
		{name: "hash instead of raw", cookies: []string{"caus_session=" + base64.RawURLEncoding.EncodeToString(result.Hash[:])}},
		{name: "duplicate", cookies: []string{"caus_session=" + result.Raw + "; caus_session=" + result.Raw}},
		{name: "conflicting duplicate", cookies: []string{"caus_session=" + result.Raw + "; caus_session=" + strings.Repeat("A", 43)}},
		{name: "duplicate headers", cookies: []string{"caus_session=" + result.Raw, "caus_session=" + result.Raw}},
		{name: "empty then valid", cookies: []string{"caus_session=; caus_session=" + result.Raw}},
		{name: "valid then empty", cookies: []string{"caus_session=" + result.Raw + "; caus_session="}},
	}
	before := meDatabaseSnapshot(t, f)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := meRequest(t, &client, http.MethodGet, f.Server.URL+"/api/me", tc.cookies...)
			assertMeError(t, response, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
			assertMeHead(t, meRequest(t, &client, http.MethodHead, f.Server.URL+"/api/me", tc.cookies...), http.StatusUnauthorized)
		})
	}
	response := meRequest(t, &client, http.MethodGet, f.Server.URL+"/api/me?caus_session="+result.Raw)
	assertMeError(t, response, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
	if before != meDatabaseSnapshot(t, f) || f.OIDC.TokenRequests.Load() != 1 {
		t.Fatal("invalid session requests mutated DB or called Google")
	}
	if strings.Contains(f.Logs.String(), result.Raw) {
		t.Fatal("session token in cookie or query leaked to logs")
	}
	t.Log("missing, malformed, ambiguous, noncanonical, unknown and hashed credentials rejected with 401 for GET and contentless HEAD without DB mutations")
}

func TestMeRevokedSessionsE2E(t *testing.T) {
	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{name: "expired", query: "UPDATE sessions SET created_at = $1, expires_at = $2", args: []any{time.Now().Add(-2 * time.Hour), time.Now().Add(-time.Hour)}},
		{name: "deleted session", query: "DELETE FROM sessions"},
		{name: "unlinked account", query: "UPDATE people SET google_sub = NULL"},
		{name: "cleared account", query: "UPDATE people SET email = NULL, google_sub = NULL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthE2EFixture(t)
			result := f.Login(t, authTestClaims{})
			if result.Response.StatusCode != http.StatusFound {
				t.Fatal("OIDC callback did not establish session")
			}
			if _, err := f.Tx.Exec(context.Background(), tc.query, tc.args...); err != nil {
				t.Fatal(err)
			}
			before := meDatabaseSnapshot(t, f)
			assertMeError(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"),
				http.StatusUnauthorized, "unauthenticated", "unauthenticated")
			assertMeHead(t, meRequest(t, f.Client, http.MethodHead, f.Server.URL+"/api/me"), http.StatusUnauthorized)
			if before != meDatabaseSnapshot(t, f) {
				t.Fatal("rejected session reads mutated DB")
			}
		})
	}
}

func TestMeRoleChangesE2E(t *testing.T) {
	f := newAuthE2EFixture(t, "ada@example.com")
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != http.StatusFound {
		t.Fatal("OIDC callback did not establish session")
	}
	for _, role := range []string{"admin", "member", "admin"} {
		if _, err := f.Tx.Exec(context.Background(), "UPDATE people SET role = $1", role); err != nil {
			t.Fatal(err)
		}
		account := meDatabaseAccount(t, f)
		before := meDatabaseSnapshot(t, f)
		assertMeAccount(t, meRequest(t, f.Client, http.MethodGet, f.Server.URL+"/api/me"), account)
		if before != meDatabaseSnapshot(t, f) {
			t.Fatal("current user role lookup mutated DB")
		}
	}
	t.Log("existing session reflects live admin promotion and demotion without a new login")
}

func TestMeMethodsE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{})
	if result.Response.StatusCode != http.StatusFound {
		t.Fatal("OIDC callback did not establish session")
	}
	client := *f.Client
	client.Jar = nil
	before := meDatabaseSnapshot(t, f)
	for _, c := range []*http.Client{f.Client, &client} {
		for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut, http.MethodPatch, http.MethodOptions} {
			response := meRequest(t, c, method, f.Server.URL+"/api/me")
			if response.Header.Get("Allow") != "GET, HEAD" {
				t.Fatal("current user route must allow only GET and HEAD")
			}
			assertMeError(t, response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}
	if before != meDatabaseSnapshot(t, f) {
		t.Fatal("unsupported methods mutated account or session")
	}
}

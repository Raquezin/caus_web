package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

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
	if err != nil || account.ID != id || account.Email != "new@example.com" || account.FirstName != "Ada" {
		t.Fatal("subject reuse failed to keep the stored profile and update the email in place")
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

func TestGoogleCallbackLinkingAndRolesE2E(t *testing.T) {
	cases := []struct {
		name     string
		subject  any
		role     string
		admins   []string
		claims   authTestClaims
		wantRole string
		status   int
	}{
		{name: "unlinked email", role: "member", status: 409},
		{name: "linked admin", subject: "admin-subject", role: "admin", status: 409},
		{name: "unlinked admin email", role: "member", admins: []string{"ada@example.com"}, status: 409},
		{name: "bootstrap promotion", subject: "verified-subject", role: "member", admins: []string{" ADA@EXAMPLE.COM "},
			claims: authTestClaims{Values: map[string]any{"hd": "example.com"}}, wantRole: "admin", status: 302},
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
			result := f.Login(t, tc.claims)
			if tc.status == 409 {
				assertSignInRedirect(t, result.Response, result.Pending.Flow.ReturnTo, "identity_conflict")
				var first, last, email, role string
				var subject *string
				if err := f.Tx.QueryRow(context.Background(),
					"SELECT first_name, last_name, email, google_sub, role FROM people WHERE id = $1", id).
					Scan(&first, &last, &email, &subject, &role); err != nil {
					t.Fatal(err)
				}
				wantSubject, _ := tc.subject.(string)
				if first != "Original" || last != "Person" || email != "ada@example.com" || role != tc.role ||
					(subject == nil) != (tc.subject == nil) || (subject != nil && *subject != wantSubject) || result.Raw != "" {
					t.Fatal("collision mutated linked account")
				}
				if people, sessions := f.counts(t); people != 1 || sessions != 0 {
					t.Fatal("collision created account or session")
				}
				return
			}
			account, err := f.Auth.CurrentUser(context.Background(), result.Raw)
			if err != nil || result.Response.StatusCode != tc.status || account.ID != id || account.Role != tc.wantRole || account.FirstName != "Original" {
				t.Fatal("verified linking or role preservation failed")
			}
		})
	}
}

func TestGoogleCallbackAdminBootstrapAuthorityE2E(t *testing.T) {
	t.Run("nonauthoritative stays member", func(t *testing.T) {
		f := newAuthE2EFixture(t, "ada@example.com")
		result := f.Login(t, authTestClaims{})
		if result.Response.StatusCode != http.StatusFound {
			t.Fatal("public signup rejected")
		}
		account, err := f.Auth.CurrentUser(context.Background(), result.Raw)
		if err != nil || account.Role != "member" {
			t.Fatal("nonauthoritative allowlisted email promoted or session lost")
		}
		var markers int
		if err := f.Tx.QueryRow(context.Background(), "SELECT count(*) FROM admin_email_bindings").Scan(&markers); err != nil {
			t.Fatal(err)
		}
		if markers != 0 {
			t.Fatal("nonauthoritative login consumed a bootstrap marker")
		}
		if people, sessions := f.counts(t); people != 1 || sessions != 1 {
			t.Fatal("nonauthoritative login did not create a member session")
		}
	})
	t.Run("hosted domain bootstraps admin", func(t *testing.T) {
		f := newAuthE2EFixture(t, "ada@example.com")
		result := f.Login(t, authTestClaims{Values: map[string]any{"hd": "example.com"}})
		if result.Response.StatusCode != http.StatusFound {
			t.Fatal("workspace login rejected")
		}
		account, err := f.Auth.CurrentUser(context.Background(), result.Raw)
		if err != nil || account.Role != "admin" {
			t.Fatal("authoritative allowlisted email not promoted")
		}
		var markers int
		if err := f.Tx.QueryRow(context.Background(), "SELECT count(*) FROM admin_email_bindings").Scan(&markers); err != nil {
			t.Fatal(err)
		}
		if markers != 1 {
			t.Fatal("authoritative bootstrap did not consume exactly one marker")
		}
	})
}

func TestGoogleCallbackHEADPostgresE2E(t *testing.T) {
	f := newAuthE2EFixture(t, "ada@example.com")
	pending := f.BeginLogin(t, authTestClaims{Values: map[string]any{"hd": "example.com"}})
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

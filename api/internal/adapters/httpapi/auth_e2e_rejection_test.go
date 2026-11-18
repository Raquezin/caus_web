package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGoogleCallbackInvalidClaimsE2E(t *testing.T) {
	cases := map[string]authTestClaims{
		"nonce":       {Values: map[string]any{"nonce": "wrong"}},
		"unverified":  {Values: map[string]any{"email_verified": false}},
		"issuer":      {Values: map[string]any{"iss": "https://other.example"}},
		"audience":    {Values: map[string]any{"aud": "other-client"}},
		"wrong azp":   {Values: map[string]any{"azp": "other-client"}},
		"multi aud":   {Values: map[string]any{"aud": []string{"test-client", "other-client"}}},
		"signature":   {InvalidSignature: true},
		"expired":     {Values: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}},
		"first name":  {Values: map[string]any{"given_name": ""}},
		"email form":  {Values: map[string]any{"email": "Ada <ada@example.com>"}},
		"schema mail": {Values: map[string]any{"email": "ada@localhost"}},
	}
	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAuthE2EFixture(t)
			result := f.Login(t, claims)
			assertSignInRedirect(t, result.Response, result.Pending.Flow.ReturnTo, "invalid_identity")
			if people, sessions := f.counts(t); people != 0 || sessions != 0 || f.OIDC.TokenRequests.Load() != 1 {
				t.Fatal("invalid verified identity mutated database or skipped real exchange")
			}
		})
	}
}

func TestGoogleCallbackRejectsFlowBeforeExchangeE2E(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*pendingAuthLogin)
		code     string
		redirect bool
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
		{name: "denied", mutate: func(p *pendingAuthLogin) { replaceCallbackCode(p, "access_denied") }, code: "oauth_denied", redirect: true},
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
			if tc.redirect {
				assertSignInRedirect(t, response, pending.Flow.ReturnTo, tc.code)
			} else {
				assertAuthError(t, response, 400, tc.code)
			}
			if people, sessions := f.counts(t); people != 0 || sessions != 0 || f.OIDC.TokenRequests.Load() != 0 {
				t.Fatal("invalid callback exchanged a code or mutated DB")
			}
			if strings.Contains(f.Logs.String(), "private-provider-detail") {
				t.Fatal("provider error leaked into logs")
			}
		})
	}
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
	assertSignInRedirect(t, response, result.Pending.Flow.ReturnTo, "invalid_identity")
	if people, sessions := f.counts(t); people != 1 || sessions != 1 {
		t.Fatal("replayed code created another session")
	}
	if f.OIDC.TokenRequests.Load() < 2 {
		t.Fatal("single-use code was not enforced by real token endpoint")
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

func TestGoogleCallbackKeysUnavailableE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	pending := f.BeginLogin(t, authTestClaims{})
	f.OIDC.KeysDown.Store(true)
	response, err := f.Client.Get(pending.CallbackURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	assertSignInRedirect(t, response, pending.Flow.ReturnTo, "auth_unavailable")
	if people, sessions := f.counts(t); people != 0 || sessions != 0 {
		t.Fatal("JWKS outage created account or session")
	}
	for _, value := range []string{"ada@example.com", "verified-subject", "test-secret", "access-marker", "refresh-marker"} {
		if strings.Contains(f.Logs.String(), value) {
			t.Fatal("JWKS outage log leaked private data")
		}
	}
	f.OIDC.KeysDown.Store(false)
	result := f.Login(t, authTestClaims{})
	defer result.Response.Body.Close()
	if result.Response.StatusCode != http.StatusFound || result.Raw == "" {
		t.Fatal("callback did not recover after JWKS outage")
	}
	if people, sessions := f.counts(t); people != 1 || sessions != 1 {
		t.Fatal("recovery did not create account and session")
	}
}

func TestGoogleCallbackWithoutFamilyNameE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	result := f.Login(t, authTestClaims{Values: map[string]any{"family_name": nil}})
	if result.Response.StatusCode != http.StatusFound || result.Raw == "" {
		t.Fatalf("login without family name: status %d", result.Response.StatusCode)
	}
	var people, blank int
	if err := f.Tx.QueryRow(context.Background(),
		"SELECT count(*), count(*) FILTER (WHERE last_name = '') FROM people").Scan(&people, &blank); err != nil {
		t.Fatal(err)
	}
	if people != 1 || blank != 1 {
		t.Fatalf("people %d with empty last name %d", people, blank)
	}
}

func TestGoogleCallbackKeepsStoredNamesE2E(t *testing.T) {
	f := newAuthE2EFixture(t)
	if result := f.Login(t, authTestClaims{}); result.Response.StatusCode != http.StatusFound {
		t.Fatal("first login failed")
	}
	if _, err := f.Tx.Exec(context.Background(), "UPDATE people SET first_name = 'Grace'"); err != nil {
		t.Fatal(err)
	}
	second := authTestClaims{Values: map[string]any{"given_name": "Other", "family_name": "Name"}}
	if result := f.Login(t, second); result.Response.StatusCode != http.StatusFound {
		t.Fatal("second login failed")
	}
	var first, last string
	if err := f.Tx.QueryRow(context.Background(), "SELECT first_name, last_name FROM people").Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	if first != "Grace" || last != "Lovelace" {
		t.Fatalf("login overwrote stored names with %q %q", first, last)
	}
}

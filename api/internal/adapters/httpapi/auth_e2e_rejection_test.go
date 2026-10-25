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
	cases := map[string]struct {
		claims authTestClaims
		status int
	}{
		"nonce":       {claims: authTestClaims{Values: map[string]any{"nonce": "wrong"}}, status: 401},
		"unverified":  {claims: authTestClaims{Values: map[string]any{"email_verified": false}}, status: 401},
		"issuer":      {claims: authTestClaims{Values: map[string]any{"iss": "https://other.example"}}, status: 401},
		"audience":    {claims: authTestClaims{Values: map[string]any{"aud": "other-client"}}, status: 401},
		"wrong azp":   {claims: authTestClaims{Values: map[string]any{"azp": "other-client"}}, status: 401},
		"multi aud":   {claims: authTestClaims{Values: map[string]any{"aud": []string{"test-client", "other-client"}}}, status: 401},
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
	assertAuthError(t, response, 503, "auth_unavailable")
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

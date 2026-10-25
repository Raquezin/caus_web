package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func TestEndpointCallbackDuplicateFlowCookiesRejectedBeforeExchange(t *testing.T) {
	cookie, flow := callbackTestFlow(t)
	otherCookie, otherFlow := callbackTestFlow(t)
	exchanges, logins := 0, 0
	provider := callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
		exchanges++
		return domain.GoogleIdentity{Subject: "verified"}, nil
	}}
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		logins++
		return domain.Account{}, "token", nil
	}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, service, AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour}, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=valid-code", nil)
	request.AddCookie(cookie)
	request.AddCookie(otherCookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	assertAuthError(t, recorder.Result(), 400, "invalid_oauth_state")
	if exchanges != 0 || logins != 0 {
		t.Fatalf("ambiguous flow reached provider or application: exchanges=%d logins=%d", exchanges, logins)
	}
	if strings.Contains(recorder.Body.String(), otherFlow.State) {
		t.Fatal("ambiguous flow leaked a state value")
	}
}

func TestEndpointCallbackSuccessKeepsFlowSecretsAndTokenOutOfRedirect(t *testing.T) {
	cookie, flow := callbackTestFlow(t)
	const returnTo = "/cuenta/perfil?tab=datos"
	const token = "session-token-secret"
	flow.ReturnTo = returnTo
	data, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	cookie.Value = base64.RawURLEncoding.EncodeToString(data)
	nonce, verifier := flow.Nonce, flow.Verifier
	provider := callbackProviderStub{exchange: func(_ context.Context, code, gotVerifier, gotNonce string) (domain.GoogleIdentity, error) {
		if code != "auth-code" || gotVerifier != verifier || gotNonce != nonce {
			t.Fatal("exchange did not receive the original code, verifier and nonce")
		}
		return domain.GoogleIdentity{Subject: "subject-1"}, nil
	}}
	service := callbackServiceStub{login: func(_ context.Context, identity domain.GoogleIdentity) (domain.Account, string, error) {
		if identity.Subject != "subject-1" {
			t.Fatal("login did not receive the verified identity")
		}
		return domain.Account{}, token, nil
	}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, service, AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour}, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") != returnTo {
		t.Fatalf("status %d location %q; wanted 302 %q", response.StatusCode, response.Header.Get("Location"), returnTo)
	}
	sessionSet := false
	for _, got := range response.Cookies() {
		if got.Name == "caus_session" {
			sessionSet = true
			if got.Value != token || got.Path != "/" || !got.HttpOnly || got.SameSite != http.SameSiteLaxMode {
				t.Fatal("session cookie missing value or security attributes")
			}
		}
	}
	if !sessionSet {
		t.Fatal("successful callback did not set the session cookie")
	}
	if strings.Contains(response.Header.Get("Location"), token) || strings.Contains(string(body), token) {
		t.Fatal("token leaked into redirect or response body")
	}
	if strings.Contains(string(body), verifier) || strings.Contains(string(body), nonce) || strings.Contains(response.Header.Get("Location"), flow.State) {
		t.Fatal("OAuth flow secret leaked into redirect or response body")
	}
}

func TestEndpointCallbackProviderInvalidIdentitySkipsLogin(t *testing.T) {
	cookie, flow := callbackTestFlow(t)
	exchanged, logins := 0, 0
	provider := callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
		exchanged++
		return domain.GoogleIdentity{Subject: "unverified"}, domain.ErrInvalidIdentity
	}}
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		logins++
		return domain.Account{}, "token", nil
	}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, service, AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour}, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	assertAuthError(t, recorder.Result(), 401, "invalid_identity")
	if exchanged != 1 || logins != 0 {
		t.Fatalf("invalid provider identity reached login: exchanged=%d logins=%d", exchanged, logins)
	}
}

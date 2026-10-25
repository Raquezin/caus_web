package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func endpointDeleteAccountIssuedSessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	flowCookie, flow := callbackTestFlow(t)
	flowCookie.Name = "__Host-caus_oauth"
	provider := callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
		return domain.GoogleIdentity{Subject: "subject"}, nil
	}}
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		return domain.Account{}, "session-token", nil
	}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, service, AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(flowCookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("callback status %d (%s)", recorder.Code, recorder.Body.String())
	}
	var issued *http.Cookie
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "__Host-caus_session" {
			issued = cookie
		}
	}
	if issued == nil {
		t.Fatalf("callback did not issue a session cookie: %v", recorder.Result().Cookies())
	}
	return issued
}

func TestEndpointDeleteAccountClearsSessionLikeLoginIssuedCookie(t *testing.T) {
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	cookies := endpointDeleteAccountSessionCookies(t, recorder)
	if len(cookies) != 1 {
		t.Fatalf("session cookies %v, want exactly one", cookies)
	}
	cleared := cookies[0]
	issued := endpointDeleteAccountIssuedSessionCookie(t)
	now := time.Now()
	if !issued.Expires.After(now) || issued.MaxAge <= 0 {
		t.Fatalf("issued cookie is not a live session cookie: %+v", issued)
	}
	if cleared.Path != issued.Path || cleared.Secure != issued.Secure ||
		cleared.HttpOnly != issued.HttpOnly || cleared.SameSite != issued.SameSite {
		t.Fatalf("cleared cookie %+v does not mirror issued cookie flags %+v", cleared, issued)
	}
	if cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.Expires.Before(now) {
		t.Fatalf("cleared cookie is not expired: %+v", cleared)
	}
}

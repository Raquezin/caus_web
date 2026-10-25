package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func boundaryLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func boundaryProvider(calls *int) callbackProviderStub {
	return callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
		*calls++
		return domain.GoogleIdentity{Subject: "verified"}, nil
	}}
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			if found != nil {
				t.Fatalf("duplicate %s cookie emitted", name)
			}
			found = cookie
		}
	}
	return found
}

func TestHTTPSCookieNamesFlagsAndClearing(t *testing.T) {
	calls := 0
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		return domain.Account{}, "raw-token", nil
	}}
	auth := NewAuth(boundaryProvider(&calls), service, AuthConfig{PublicURL: "https://app.example", SessionTTL: time.Hour}, boundaryLogger())
	router := NewRouter(nil, nil, boundaryLogger(), auth)

	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/api/auth/google/login", nil))
	flow := responseCookie(t, login.Result(), "__Host-caus_oauth")
	if flow == nil || !flow.Secure || !flow.HttpOnly || flow.SameSite != http.SameSiteLaxMode ||
		flow.Domain != "" || flow.Path != "/" || len(login.Result().Cookies()) != 1 {
		t.Fatalf("HTTPS flow cookie not host-prefixed with secure defaults: %+v", login.Result().Cookies())
	}
	if legacy := responseCookie(t, login.Result(), "caus_oauth"); legacy != nil {
		t.Fatal("HTTPS login emitted the unprefixed flow cookie")
	}

	callback := httptest.NewRecorder()
	flowCookie, oauth := callbackTestFlow(t)
	flowCookie.Name = "__Host-caus_oauth"
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+oauth.State+"&code=test", nil)
	request.AddCookie(flowCookie)
	router.ServeHTTP(callback, request)
	if callback.Code != http.StatusFound {
		t.Fatalf("prefixed flow cookie rejected: %d", callback.Code)
	}
	session := responseCookie(t, callback.Result(), "__Host-caus_session")
	cleared := responseCookie(t, callback.Result(), "__Host-caus_oauth")
	if session == nil || session.Value != "raw-token" || !session.Secure || !session.HttpOnly ||
		session.SameSite != http.SameSiteLaxMode || session.Domain != "" || session.Path != "/" {
		t.Fatalf("HTTPS session cookie flags incorrect: %+v", session)
	}
	if cleared == nil || cleared.Value != "" || cleared.MaxAge != -1 || cleared.Path != "/" || !cleared.Secure {
		t.Fatalf("HTTPS flow cookie cleared with mismatched attributes: %+v", cleared)
	}

	logoutAuth := NewAuth(nil, &endpointLogoutService{}, auth.cfg, boundaryLogger())
	backend := NewBackend(nil, logoutAuth, boundaryLogger())
	logout := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.Header.Set("Origin", "https://app.example")
	logoutRequest.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "raw-token"})
	backend.logout(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status %d", logout.Code)
	}
	clearedSession := responseCookie(t, logout.Result(), "__Host-caus_session")
	if clearedSession == nil || clearedSession.Value != "" || clearedSession.MaxAge != -1 ||
		clearedSession.Path != "/" || !clearedSession.Secure {
		t.Fatalf("HTTPS session cleared with mismatched attributes: %+v", clearedSession)
	}
}

func TestHTTPSIgnoresUnprefixedSessionCookie(t *testing.T) {
	service := &stubBackendAuth{account: domain.Account{Role: "member"}}
	auth := newStubBackendAuth(t, service, "https://app.example")
	handler := auth.requireUser(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	legacy := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	legacy.AddCookie(&http.Cookie{Name: "caus_session", Value: "session-token"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, legacy)
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")

	mixed := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	mixed.AddCookie(&http.Cookie{Name: "caus_session", Value: "legacy-token"})
	mixed.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, mixed)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected-name session rejected alongside ignored cookie: %d", recorder.Code)
	}

	duplicate := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	duplicate.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "one"})
	duplicate.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "two"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, duplicate)
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if service.callCount() != 1 {
		t.Fatalf("rejected or ignored cookies reached the service %d times", service.callCount())
	}
}

func TestHTTPSCallbackIgnoresUnprefixedOAuthCookie(t *testing.T) {
	calls, logins := 0, 0
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		logins++
		return domain.Account{}, "raw-token", nil
	}}
	auth := NewAuth(boundaryProvider(&calls), service, AuthConfig{PublicURL: "https://app.example", SessionTTL: time.Hour}, boundaryLogger())
	router := NewRouter(nil, nil, boundaryLogger(), auth)
	flowCookie, flow := callbackTestFlow(t)

	legacy := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=test", nil)
	legacy.AddCookie(flowCookie)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, legacy)
	if recorder.Code != 400 || errorCode(t, recorder) != "invalid_oauth_state" {
		t.Fatalf("unprefixed flow cookie accepted: %d", recorder.Code)
	}
	if cleared := responseCookie(t, recorder.Result(), "__Host-caus_oauth"); cleared == nil ||
		cleared.MaxAge != -1 || cleared.Path != "/" {
		t.Fatal("HTTPS callback did not clear the prefixed flow cookie")
	}

	flowCookie.Name = "__Host-caus_oauth"
	mixed := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=test", nil)
	mixed.AddCookie(&http.Cookie{Name: "caus_oauth", Value: "unrelated"})
	mixed.AddCookie(flowCookie)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, mixed)
	if recorder.Code != http.StatusFound {
		t.Fatalf("prefixed flow cookie rejected alongside ignored cookie: %d", recorder.Code)
	}

	duplicate := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=test", nil)
	duplicate.AddCookie(flowCookie)
	duplicate.AddCookie(flowCookie)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, duplicate)
	if recorder.Code != 400 || errorCode(t, recorder) != "invalid_oauth_state" {
		t.Fatalf("duplicate prefixed flow cookie accepted: %d", recorder.Code)
	}
	if calls != 1 || logins != 1 {
		t.Fatalf("unexpected exchange %d or login %d calls", calls, logins)
	}
}

func TestCORSAllowsOnlyConfiguredOrigin(t *testing.T) {
	for _, publicURL := range []string{"http://localhost:8080", "https://app.example", ""} {
		t.Run(publicURL, func(t *testing.T) {
			service := &stubBackendAuth{account: domain.Account{Role: "member"}}
			auth := newStubBackendAuth(t, service, publicURL)
			backend := NewBackend(nil, auth, boundaryLogger())
			router := NewRouterWithBackend(&fakePeople{}, nil, boundaryLogger(), auth, backend)
			sessionName := "caus_session"
			if strings.HasPrefix(publicURL, "https://") {
				sessionName = "__Host-caus_session"
			}
			denied := []string{"https://clubalgoritmiaus.es", "http://localhost:4321", "https://localhost:8080", "null", "https://other.example"}
			for _, origin := range denied {
				if origin == publicURL {
					continue
				}
				request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
				request.Header.Set("Origin", origin)
				request.AddCookie(&http.Cookie{Name: sessionName, Value: "session-token"})
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != "cors_origin_denied" ||
					recorder.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Fatalf("origin %q not denied: %d", origin, recorder.Code)
				}
			}
			if service.callCount() != 0 {
				t.Fatalf("denied origins reached the service %d times", service.callCount())
			}
			if publicURL != "" {
				request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
				request.Header.Set("Origin", publicURL)
				request.AddCookie(&http.Cookie{Name: sessionName, Value: "session-token"})
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK || recorder.Header().Get("Access-Control-Allow-Origin") != publicURL {
					t.Fatalf("configured origin rejected: %d", recorder.Code)
				}
				if service.callCount() != 1 {
					t.Fatalf("configured origin reached the service %d times", service.callCount())
				}
			}
			public := httptest.NewRecorder()
			router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
			if public.Code != http.StatusOK {
				t.Fatal("origin-less public request rejected")
			}
			if service.callCount() > 1 {
				t.Fatalf("unexpected service calls %d", service.callCount())
			}
		})
	}
}

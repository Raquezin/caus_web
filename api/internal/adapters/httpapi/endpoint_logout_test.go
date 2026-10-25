package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const endpointLogoutPath = "/api/auth/logout"

type endpointLogoutService struct {
	AuthService
	account domain.Account
	err     error
	calls   int
	tokens  []string
	ctx     context.Context
}

func (s *endpointLogoutService) CurrentUser(context.Context, string) (domain.Account, error) {
	return s.account, nil
}

func (s *endpointLogoutService) Logout(ctx context.Context, token string) error {
	s.calls++
	s.tokens = append(s.tokens, token)
	s.ctx = ctx
	return s.err
}

func (s *endpointLogoutService) endpointLogoutCalls() int {
	return s.calls
}

func (s *endpointLogoutService) endpointLogoutRecordedTokens() []string {
	return append([]string(nil), s.tokens...)
}

func (s *endpointLogoutService) endpointLogoutContext() context.Context {
	return s.ctx
}

func endpointLogoutAccount() domain.Account {
	return domain.Account{ID: uuid.MustParse("b7f4d6d0-3a1e-4f8a-9d55-6f2c1f3ab8e4"), Email: "ada@example.com", Role: "member"}
}

func endpointLogoutBackend(t *testing.T, service AuthService) (*Auth, *Backend) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(nil, service, AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	return auth, NewBackend(nil, auth, logger)
}

func endpointLogoutHandler(auth *Auth, backend *Backend) http.Handler {
	return endpointMethods(map[string]http.Handler{
		http.MethodPost: auth.requireUser(auth.requireSameOrigin(http.HandlerFunc(backend.logout))),
	})
}

func endpointLogoutRequest(method string, origins []string, session bool) *http.Request {
	request := httptest.NewRequest(method, endpointLogoutPath, nil)
	if session {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	}
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	return request
}

func endpointLogoutSessionCookies(t *testing.T, recorder *httptest.ResponseRecorder) []*http.Cookie {
	t.Helper()
	found := make([]*http.Cookie, 0, 1)
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "__Host-caus_session" {
			found = append(found, cookie)
		}
	}
	return found
}

func assertEndpointLogoutError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status %d, want %d (%s)", recorder.Code, status, recorder.Body.String())
	}
	if got := errorCode(t, recorder); got != code {
		t.Fatalf("code %q, want %q", got, code)
	}
}

func TestEndpointLogoutMissingSession(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount()}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointLogoutRequest(http.MethodPost, nil, false))
	assertEndpointLogoutError(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if cookies := endpointLogoutSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("missing session emitted a cookie: %v", cookies)
	}
	if service.endpointLogoutCalls() != 0 {
		t.Fatalf("sessionless logout reached the service %d times", service.endpointLogoutCalls())
	}
}

func TestEndpointLogoutRejectsUnusableCookies(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount()}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	empty := httptest.NewRequest(http.MethodPost, endpointLogoutPath, nil)
	empty.Header.Set("Origin", "https://example.test")
	empty.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: ""})
	duplicate := httptest.NewRequest(http.MethodPost, endpointLogoutPath, nil)
	duplicate.Header.Set("Origin", "https://example.test")
	duplicate.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	duplicate.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	for name, request := range map[string]*http.Request{"empty": empty, "duplicate": duplicate} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertEndpointLogoutError(t, recorder, http.StatusUnauthorized, "unauthenticated")
		if cookies := endpointLogoutSessionCookies(t, recorder); len(cookies) != 0 {
			t.Fatalf("%s cookie emitted a session cookie: %v", name, cookies)
		}
	}
	if service.endpointLogoutCalls() != 0 {
		t.Fatalf("unusable cookies reached the service %d times", service.endpointLogoutCalls())
	}
}

func TestEndpointLogoutRequiresSameOrigin(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount()}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	for name, origins := range map[string][]string{
		"missing":   nil,
		"foreign":   {"https://attacker.test"},
		"duplicate": {"https://example.test", "https://example.test"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointLogoutRequest(http.MethodPost, origins, true))
		assertEndpointLogoutError(t, recorder, http.StatusForbidden, "origin_denied")
		if cookies := endpointLogoutSessionCookies(t, recorder); len(cookies) != 0 {
			t.Fatalf("%s origin emitted a session cookie: %v", name, cookies)
		}
	}
	if service.endpointLogoutCalls() != 0 {
		t.Fatalf("denied origins reached the service %d times", service.endpointLogoutCalls())
	}
}

func TestEndpointLogoutClearsSession(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount()}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointLogoutRequest(http.MethodPost, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("logout returned a body: %s", recorder.Body.String())
	}
	if calls := service.endpointLogoutCalls(); calls != 1 {
		t.Fatalf("Logout calls %d, want 1", calls)
	}
	tokens := service.endpointLogoutRecordedTokens()
	if len(tokens) != 1 || tokens[0] != "session-token" {
		t.Fatalf("Logout tokens %v", tokens)
	}
	cookies := endpointLogoutSessionCookies(t, recorder)
	if len(cookies) != 1 {
		t.Fatalf("session cookies %v", cookies)
	}
	cookie := cookies[0]
	if cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie %+v", cookie)
	}
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie attributes %+v", cookie)
	}
	if cookie.Expires.IsZero() || !cookie.Expires.Before(time.Now()) {
		t.Fatalf("cookie expiry %v", cookie.Expires)
	}
	for name, values := range recorder.Header() {
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), "bearer") || strings.Contains(value, "session-token") {
				t.Fatalf("logout leaked the token in header %s: %s", name, value)
			}
		}
	}
	body := recorder.Body.String()
	if strings.Contains(strings.ToLower(body), "bearer") || strings.Contains(body, "session-token") {
		t.Fatalf("logout leaked the token in body: %s", body)
	}
	ctx := service.endpointLogoutContext()
	if ctx == nil {
		t.Fatal("Logout received a nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("Logout context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > lookupTimeout || remaining < lookupTimeout-2*time.Second {
		t.Fatalf("Logout deadline remaining %v, want about %v", remaining, lookupTimeout)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("Logout context error %v after handler returned", ctx.Err())
	}
}

func TestEndpointLogoutUnauthenticatedSession(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount(), err: domain.ErrUnauthenticated}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointLogoutRequest(http.MethodPost, []string{"https://example.test"}, true))
	assertEndpointLogoutError(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if cookies := endpointLogoutSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("expired session cleared a cookie: %v", cookies)
	}
	if calls := service.endpointLogoutCalls(); calls != 1 {
		t.Fatalf("Logout calls %d, want 1", calls)
	}
}

func TestEndpointLogoutBackendFailure(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount(), err: errors.New("database password rejected")}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointLogoutRequest(http.MethodPost, []string{"https://example.test"}, true))
	assertEndpointLogoutError(t, recorder, http.StatusInternalServerError, "internal_error")
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("logout leaked backend detail: %s", recorder.Body.String())
	}
	if cookies := endpointLogoutSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("failed logout cleared a cookie: %v", cookies)
	}
}

func TestEndpointLogoutRejectsWrongMethods(t *testing.T) {
	service := &endpointLogoutService{account: endpointLogoutAccount()}
	auth, backend := endpointLogoutBackend(t, service)
	handler := endpointLogoutHandler(auth, backend)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointLogoutRequest(method, []string{"https://example.test"}, true))
		if allow := recorder.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("%s Allow %q", method, allow)
		}
		assertEndpointLogoutError(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
	}
	if service.endpointLogoutCalls() != 0 {
		t.Fatalf("wrong methods reached the service %d times", service.endpointLogoutCalls())
	}
}

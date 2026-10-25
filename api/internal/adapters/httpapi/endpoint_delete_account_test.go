package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const endpointDeleteAccountPath = "/api/me"

type endpointDeleteAccountService struct {
	BackendService
	mu    sync.Mutex
	ids   []uuid.UUID
	err   error
	calls int
}

func (s *endpointDeleteAccountService) DeleteAccount(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ids = append(s.ids, id)
	return s.err
}

func (s *endpointDeleteAccountService) endpointDeleteAccountCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *endpointDeleteAccountService) endpointDeleteAccountIDs() []uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.ids...)
}

func endpointDeleteAccountAccount() domain.Account {
	return domain.Account{ID: uuid.MustParse("6e2c1f4a-8b3d-4c59-9a70-1f2d3e4b5c6a"), Email: "ada@example.com", Role: "admin"}
}

func endpointDeleteAccountHandler(t *testing.T, authService AuthService, deleteService BackendService, logs io.Writer) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	auth := NewAuth(nil, authService, AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	backend := NewBackend(deleteService, auth, logger)
	return auth.requireUser(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodDelete: http.HandlerFunc(backend.deleteAccount),
	})))
}

func endpointDeleteAccountRequest(method string, origins []string, session bool) *http.Request {
	var request *http.Request
	if session {
		request = sessionRequest(method, endpointDeleteAccountPath)
	} else {
		request = httptest.NewRequest(method, endpointDeleteAccountPath, nil)
	}
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	return request
}

func endpointDeleteAccountSessionCookies(t *testing.T, recorder *httptest.ResponseRecorder) []*http.Cookie {
	t.Helper()
	found := make([]*http.Cookie, 0, 1)
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "__Host-caus_session" {
			found = append(found, cookie)
		}
	}
	return found
}

func TestEndpointDeleteAccountClearsSession(t *testing.T) {
	account := endpointDeleteAccountAccount()
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: account}, deleteService, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("delete returned a body: %s", recorder.Body.String())
	}
	if calls := deleteService.endpointDeleteAccountCalls(); calls != 1 {
		t.Fatalf("DeleteAccount calls %d, want 1", calls)
	}
	ids := deleteService.endpointDeleteAccountIDs()
	if len(ids) != 1 || ids[0] != account.ID {
		t.Fatalf("DeleteAccount ids %v, want context subject %v", ids, account.ID)
	}
	cookies := endpointDeleteAccountSessionCookies(t, recorder)
	if len(cookies) != 1 {
		t.Fatalf("session cookies %v", cookies)
	}
	if cookies[0].Value != "" || cookies[0].MaxAge >= 0 {
		t.Fatalf("cleared cookie %+v", cookies[0])
	}
	if strings.Contains(recorder.Body.String(), "session-token") {
		t.Fatalf("delete leaked the token: %s", recorder.Body.String())
	}
}

func TestEndpointDeleteAccountBackendFailureKeepsCookie(t *testing.T) {
	var logs bytes.Buffer
	deleteService := &endpointDeleteAccountService{err: errors.New("database password rejected")}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, &logs)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if strings.Contains(recorder.Body.String(), "password") || strings.Contains(logs.String(), "password") {
		t.Fatalf("backend failure leaked detail: body=%s logs=%s", recorder.Body.String(), logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
	if cookies := endpointDeleteAccountSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("failed delete cleared a cookie: %v", cookies)
	}
}

func TestEndpointDeleteAccountMissingPerson(t *testing.T) {
	deleteService := &endpointDeleteAccountService{err: domain.ErrNotFound}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	assertBackendError(t, recorder, http.StatusNotFound, "not_found")
	if cookies := endpointDeleteAccountSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("missing person cleared a cookie: %v", cookies)
	}
}

func TestEndpointDeleteAccountExpiredSession(t *testing.T) {
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{err: domain.ErrUnauthenticated}, deleteService, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, []string{"https://example.test"}, true))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if calls := deleteService.endpointDeleteAccountCalls(); calls != 0 {
		t.Fatalf("expired session reached DeleteAccount %d times", calls)
	}
	if cookies := endpointDeleteAccountSessionCookies(t, recorder); len(cookies) != 0 {
		t.Fatalf("expired session cleared a cookie: %v", cookies)
	}
}

func TestEndpointDeleteAccountRequiresUserBeforeOrigin(t *testing.T) {
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, nil, false))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")
	if calls := deleteService.endpointDeleteAccountCalls(); calls != 0 {
		t.Fatalf("unauthenticated request reached DeleteAccount %d times", calls)
	}
}

func TestEndpointDeleteAccountRequiresSameOrigin(t *testing.T) {
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, io.Discard)
	for name, origins := range map[string][]string{
		"missing":   nil,
		"foreign":   {"https://attacker.test"},
		"duplicate": {"https://example.test", "https://example.test"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointDeleteAccountRequest(http.MethodDelete, origins, true))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s origin status %d (%s)", name, recorder.Code, recorder.Body.String())
		}
		assertBackendError(t, recorder, http.StatusForbidden, "origin_denied")
		if cookies := endpointDeleteAccountSessionCookies(t, recorder); len(cookies) != 0 {
			t.Fatalf("%s origin cleared a cookie: %v", name, cookies)
		}
	}
	if calls := deleteService.endpointDeleteAccountCalls(); calls != 0 {
		t.Fatalf("denied origins reached DeleteAccount %d times", calls)
	}
}

func TestEndpointDeleteAccountRejectsWrongMethods(t *testing.T) {
	deleteService := &endpointDeleteAccountService{}
	handler := endpointDeleteAccountHandler(t, &stubBackendAuth{account: endpointDeleteAccountAccount()}, deleteService, io.Discard)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointDeleteAccountRequest(method, []string{"https://example.test"}, true))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status %d (%s)", method, recorder.Code, recorder.Body.String())
		}
		if allow := recorder.Header().Get("Allow"); allow != http.MethodDelete {
			t.Fatalf("%s Allow %q", method, allow)
		}
		assertBackendError(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
	}
	if calls := deleteService.endpointDeleteAccountCalls(); calls != 0 {
		t.Fatalf("wrong methods reached DeleteAccount %d times", calls)
	}
}

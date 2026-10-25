package httpapi

import (
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

type stubBackendAuth struct {
	mu      sync.Mutex
	account domain.Account
	err     error
	calls   int
}

func (s *stubBackendAuth) Login(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
	return domain.Account{}, "", errors.New("login unsupported")
}

func (s *stubBackendAuth) CurrentUser(context.Context, string) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return domain.Account{}, s.err
	}
	return s.account, nil
}

func (s *stubBackendAuth) Logout(context.Context, string) error {
	return errors.New("logout unsupported")
}

func (s *stubBackendAuth) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newStubBackendAuth(t *testing.T, service AuthService, publicURL string) *Auth {
	t.Helper()
	return NewAuth(nil, service, AuthConfig{PublicURL: publicURL, SessionTTL: time.Hour},
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func sessionRequest(method, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	return request
}

func assertBackendError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status %d, want %d (%s)", recorder.Code, status, recorder.Body.String())
	}
	if got := errorCode(t, recorder); got != code {
		t.Fatalf("code %q, want %q", got, code)
	}
}

func TestRequireUserAuthenticates(t *testing.T) {
	account := domain.Account{ID: uuid.New(), Email: "ada@example.com", Role: "member"}
	service := &stubBackendAuth{account: account}
	auth := newStubBackendAuth(t, service, "https://example.test")
	handler := auth.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		found, ok := accountFromContext(r.Context())
		if !ok || found != account {
			t.Errorf("context account %+v %v", found, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/me"))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d", recorder.Code)
	}
	if service.callCount() != 1 {
		t.Fatalf("CurrentUser calls %d", service.callCount())
	}
}

func TestRequireUserRejectsMissingOrDuplicateCookies(t *testing.T) {
	service := &stubBackendAuth{account: domain.Account{ID: uuid.New()}}
	auth := newStubBackendAuth(t, service, "https://example.test")
	handler := auth.requireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler reached without a session")
	}))
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	assertBackendError(t, missing, http.StatusUnauthorized, "unauthenticated")
	duplicate := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	duplicate.Header.Add("Cookie", "__Host-caus_session=one")
	duplicate.Header.Add("Cookie", "__Host-caus_session=two")
	duplicateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(duplicateRecorder, duplicate)
	assertBackendError(t, duplicateRecorder, http.StatusUnauthorized, "unauthenticated")
	empty := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	empty.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: ""})
	emptyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(emptyRecorder, empty)
	assertBackendError(t, emptyRecorder, http.StatusUnauthorized, "unauthenticated")
	if service.callCount() != 0 {
		t.Fatalf("invalid cookies reached the service %d times", service.callCount())
	}
}

func TestRequireUserServiceErrors(t *testing.T) {
	auth := newStubBackendAuth(t, &stubBackendAuth{err: domain.ErrUnauthenticated}, "https://example.test")
	recorder := httptest.NewRecorder()
	auth.requireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("expired session reached the handler")
	})).ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/me"))
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")

	auth = newStubBackendAuth(t, &stubBackendAuth{err: errors.New("database password rejected")}, "https://example.test")
	recorder = httptest.NewRecorder()
	auth.requireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("backend failure reached the handler")
	})).ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/me"))
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("leaked backend failure: %s", recorder.Body.String())
	}

	auth = newStubBackendAuth(t, nil, "https://example.test")
	recorder = httptest.NewRecorder()
	auth.requireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("missing service reached the handler")
	})).ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/me"))
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
}

func TestRequireAdminAndAccountCaching(t *testing.T) {
	member := domain.Account{ID: uuid.New(), Role: "member"}
	service := &stubBackendAuth{account: member}
	auth := newStubBackendAuth(t, service, "https://example.test")
	recorder := httptest.NewRecorder()
	auth.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("member reached the admin handler")
	})).ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/talks"))
	assertBackendError(t, recorder, http.StatusForbidden, "forbidden")

	admin := domain.Account{ID: uuid.New(), Role: "admin"}
	service = &stubBackendAuth{account: admin}
	auth = newStubBackendAuth(t, service, "https://example.test")
	handler := auth.requireUser(auth.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		found, ok := accountFromContext(r.Context())
		if !ok || found != admin {
			t.Errorf("cached account %+v %v", found, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/talks"))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("admin status %d", recorder.Code)
	}
	if service.callCount() != 1 {
		t.Fatalf("account resolved %d times, want 1", service.callCount())
	}
}

func TestRequireSameOrigin(t *testing.T) {
	auth := newStubBackendAuth(t, &stubBackendAuth{}, "https://example.test")
	accepted := httptest.NewRequest(http.MethodPost, "/api/talks", nil)
	accepted.Header.Set("Origin", "https://example.test")
	recorder := httptest.NewRecorder()
	auth.requireSameOrigin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, accepted)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("matching origin status %d", recorder.Code)
	}
	for name, origins := range map[string][]string{
		"missing":   nil,
		"wrong":     {"https://attacker.test"},
		"duplicate": {"https://example.test", "https://example.test"},
		"prefixed":  {"https://example.test.evil.test"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/talks", nil)
		for _, origin := range origins {
			request.Header.Add("Origin", origin)
		}
		recorder := httptest.NewRecorder()
		auth.requireSameOrigin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Errorf("%s origin reached the handler", name)
		})).ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s origin status %d", name, recorder.Code)
		}
	}
	unconfigured := newStubBackendAuth(t, &stubBackendAuth{}, "")
	recorder = httptest.NewRecorder()
	unconfigured.requireSameOrigin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("unconfigured origin reached the handler")
	})).ServeHTTP(recorder, accepted)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unconfigured origin status %d", recorder.Code)
	}
}

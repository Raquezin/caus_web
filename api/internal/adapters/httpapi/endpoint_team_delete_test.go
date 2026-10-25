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

const endpointTeamDeleteOrigin = "https://example.test"

type endpointTeamDeleteService struct {
	BackendService
	mu        sync.Mutex
	teams     map[uuid.UUID]bool
	ids       []uuid.UUID
	remaining time.Duration
	err       error
	calls     int
}

func (s *endpointTeamDeleteService) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.ids = append(s.ids, id)
	if deadline, ok := ctx.Deadline(); ok {
		s.remaining = time.Until(deadline)
	}
	if s.err != nil {
		return s.err
	}
	if !s.teams[id] {
		return domain.ErrNotFound
	}
	delete(s.teams, id)
	return nil
}

func (s *endpointTeamDeleteService) snapshot() (int, []uuid.UUID, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, append([]uuid.UUID(nil), s.ids...), s.remaining
}

func endpointTeamDeleteBackend(t *testing.T, authService AuthService, backendService BackendService,
	logs *bytes.Buffer) (*Auth, *Backend) {
	t.Helper()
	writer := io.Writer(io.Discard)
	if logs != nil {
		writer = logs
	}
	logger := slog.New(slog.NewJSONHandler(writer, nil))
	auth := NewAuth(nil, authService, AuthConfig{PublicURL: endpointTeamDeleteOrigin, SessionTTL: time.Hour}, logger)
	return auth, NewBackend(backendService, auth, logger)
}

func endpointTeamDeleteHandler(auth *Auth, backend *Backend) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/admin/teams/{id}", auth.requireAdmin(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodDelete: http.HandlerFunc(backend.deleteTeam),
	}))))
	return mux
}

func endpointTeamDeleteRequest(method, id string, origins []string, session bool) *http.Request {
	target := "/api/admin/teams/" + id
	var request *http.Request
	if session {
		request = sessionRequest(method, target)
	} else {
		request = httptest.NewRequest(method, target, nil)
	}
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	return request
}

func endpointTeamDeleteAdminAuth() AuthService {
	return &stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: "admin"}}
}

func TestEndpointTeamDeleteAdminRemovesTeam(t *testing.T) {
	id := uuid.New()
	service := &endpointTeamDeleteService{teams: map[uuid.UUID]bool{id: true}}
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, nil)
	handler := endpointTeamDeleteHandler(auth, backend)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete, id.String(),
		[]string{endpointTeamDeleteOrigin}, true))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("delete returned a body: %s", recorder.Body.String())
	}
	calls, ids, remaining := service.snapshot()
	if calls != 1 || len(ids) != 1 || ids[0] != id {
		t.Fatalf("service calls %d ids %v", calls, ids)
	}
	if remaining <= 0 || remaining > 5*time.Second {
		t.Fatalf("service context deadline remaining %v, want positive and at most %v", remaining, 5*time.Second)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete, id.String(),
		[]string{endpointTeamDeleteOrigin}, true))
	assertBackendError(t, recorder, http.StatusNotFound, "not_found")
	if calls, _, _ := service.snapshot(); calls != 2 {
		t.Fatalf("repeat delete calls %d, want 2", calls)
	}
}

func TestEndpointTeamDeleteInvalidID(t *testing.T) {
	service := &endpointTeamDeleteService{teams: map[uuid.UUID]bool{}}
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, nil)
	handler := endpointTeamDeleteHandler(auth, backend)
	for _, id := range []string{"not-a-uuid", uuid.Nil.String(), "123e4567"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete, id,
			[]string{endpointTeamDeleteOrigin}, true))
		assertBackendError(t, recorder, http.StatusBadRequest, "invalid_id")
	}
	if calls, _, _ := service.snapshot(); calls != 0 {
		t.Fatalf("invalid ids reached the service %d times", calls)
	}
}

func TestEndpointTeamDeleteRequiresAdmin(t *testing.T) {
	service := &endpointTeamDeleteService{teams: map[uuid.UUID]bool{}}
	recorder := httptest.NewRecorder()
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, nil)
	endpointTeamDeleteHandler(auth, backend).ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete,
		uuid.NewString(), []string{endpointTeamDeleteOrigin}, false))
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")

	member := &stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: "member"}}
	auth, backend = endpointTeamDeleteBackend(t, member, service, nil)
	recorder = httptest.NewRecorder()
	endpointTeamDeleteHandler(auth, backend).ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete,
		uuid.NewString(), []string{endpointTeamDeleteOrigin}, true))
	assertBackendError(t, recorder, http.StatusForbidden, "forbidden")

	expired := &stubBackendAuth{err: domain.ErrUnauthenticated}
	auth, backend = endpointTeamDeleteBackend(t, expired, service, nil)
	recorder = httptest.NewRecorder()
	endpointTeamDeleteHandler(auth, backend).ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete,
		uuid.NewString(), []string{endpointTeamDeleteOrigin}, true))
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")

	if calls, _, _ := service.snapshot(); calls != 0 {
		t.Fatalf("unauthorized requests reached the service %d times", calls)
	}
}

func TestEndpointTeamDeleteRequiresSameOrigin(t *testing.T) {
	service := &endpointTeamDeleteService{teams: map[uuid.UUID]bool{}}
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, nil)
	handler := endpointTeamDeleteHandler(auth, backend)
	for name, origins := range map[string][]string{
		"missing":   nil,
		"foreign":   {"https://attacker.test"},
		"duplicate": {endpointTeamDeleteOrigin, endpointTeamDeleteOrigin},
		"prefixed":  {endpointTeamDeleteOrigin + ".evil.test"},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete, uuid.NewString(), origins, true))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s origin status %d (%s)", name, recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "origin_denied" {
			t.Fatalf("%s origin code %q", name, code)
		}
	}
	if calls, _, _ := service.snapshot(); calls != 0 {
		t.Fatalf("denied origins reached the service %d times", calls)
	}
}

func TestEndpointTeamDeleteSanitizesFailure(t *testing.T) {
	var logs bytes.Buffer
	service := &endpointTeamDeleteService{err: errors.New("database password rejected")}
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, &logs)
	recorder := httptest.NewRecorder()
	endpointTeamDeleteHandler(auth, backend).ServeHTTP(recorder, endpointTeamDeleteRequest(http.MethodDelete,
		uuid.NewString(), []string{endpointTeamDeleteOrigin}, true))
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	for _, secret := range []string{"password", "database"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("body leaked %q: %s", secret, recorder.Body.String())
		}
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logs leaked %q: %s", secret, logs.String())
		}
	}
	if !strings.Contains(logs.String(), `"error_type"`) || !strings.Contains(logs.String(), "/api/admin/teams/{id}") {
		t.Fatalf("logs missing error type or operation: %s", logs.String())
	}
}

func TestEndpointTeamDeleteRejectsWrongMethods(t *testing.T) {
	service := &endpointTeamDeleteService{teams: map[uuid.UUID]bool{}}
	auth, backend := endpointTeamDeleteBackend(t, endpointTeamDeleteAdminAuth(), service, nil)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch} {
		recorder := httptest.NewRecorder()
		endpointTeamDeleteHandler(auth, backend).ServeHTTP(recorder, endpointTeamDeleteRequest(method,
			uuid.NewString(), []string{endpointTeamDeleteOrigin}, true))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status %d (%s)", method, recorder.Code, recorder.Body.String())
		}
		if allow := recorder.Header().Get("Allow"); allow != http.MethodDelete {
			t.Fatalf("%s Allow %q", method, allow)
		}
		assertBackendError(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
	}
	if calls, _, _ := service.snapshot(); calls != 0 {
		t.Fatalf("wrong methods reached the service %d times", calls)
	}
}

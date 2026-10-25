package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubEndpointAdminCompetitionsListService struct {
	BackendService
	mu          sync.Mutex
	calls       int
	page        domain.Page
	deadline    time.Time
	hasDeadline bool
	ctxErr      error
	result      domain.CompetitionPage
	err         error
}

func (s *stubEndpointAdminCompetitionsListService) ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.page = page
	s.deadline, s.hasDeadline = ctx.Deadline()
	s.ctxErr = ctx.Err()
	return s.result, s.err
}

func (s *stubEndpointAdminCompetitionsListService) snapshot() (calls int, page domain.Page, deadline time.Time, hasDeadline bool, ctxErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.page, s.deadline, s.hasDeadline, s.ctxErr
}

func endpointAdminCompetitionsListAuth(t *testing.T, role string) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Email: "ada@example.com", Role: role},
	}, "https://example.test")
}

func endpointAdminCompetitionsListHandler(auth *Auth, service BackendService, log *slog.Logger) http.Handler {
	backend := NewBackend(service, auth, log)
	return auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.adminListCompetitions),
	}))
}

func endpointAdminCompetitionsListFixture() domain.Competition {
	created := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	return domain.Competition{
		ID:          uuid.MustParse("8a1f6d3c-2b4e-4c9a-9f7d-1e2b3c4d5e6f"),
		Name:        "Caus CTF",
		Description: "Capture the flag",
		Date:        time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC),
		Location:    "Madrid",
		TeamSize:    4,
		CreatedAt:   created,
		UpdatedAt:   created.Add(2 * time.Hour),
	}
}

func TestEndpointAdminCompetitionsList(t *testing.T) {
	t.Run("missing session returns 401 before service", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/competitions", nil))
		assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")
		if calls, _, _, _, _ := service.snapshot(); calls != 0 {
			t.Fatalf("sessionless request reached the service %d times", calls)
		}
	})

	t.Run("member returns 403 before service", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "member"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions"))
		assertBackendError(t, recorder, http.StatusForbidden, "forbidden")
		if calls, _, _, _, _ := service.snapshot(); calls != 0 {
			t.Fatalf("member request reached the service %d times", calls)
		}
	})

	t.Run("admin returns standard data and pagination", func(t *testing.T) {
		competition := endpointAdminCompetitionsListFixture()
		service := &stubEndpointAdminCompetitionsListService{
			result: domain.CompetitionPage{Competitions: []domain.Competition{competition}},
		}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
		}
		if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("Content-Type %q", contentType)
		}
		var payload struct {
			Data       []map[string]any `json:"data"`
			Pagination struct {
				Limit   int  `json:"limit"`
				Offset  int  `json:"offset"`
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
		}
		if len(payload.Data) != 1 {
			t.Fatalf("data length %d (%s)", len(payload.Data), recorder.Body.String())
		}
		item := payload.Data[0]
		if len(item) != 8 {
			t.Fatalf("competition exposes %d fields: %v", len(item), item)
		}
		if item["id"] != competition.ID.String() || item["name"] != "Caus CTF" ||
			item["description"] != "Capture the flag" || item["location"] != "Madrid" ||
			item["date"] != "2026-09-12T10:00:00" || item["team_size"] != float64(4) {
			t.Fatalf("competition payload %v", item)
		}
		if payload.Pagination.Limit != domain.DefaultPageLimit || payload.Pagination.Offset != 0 || payload.Pagination.HasMore {
			t.Fatalf("pagination %+v", payload.Pagination)
		}
		if calls, page, _, _, _ := service.snapshot(); calls != 1 || page != (domain.Page{Limit: domain.DefaultPageLimit}) {
			t.Fatalf("service calls %d page %+v", calls, page)
		}
	})

	t.Run("invalid page returns 400 before service", func(t *testing.T) {
		for _, target := range []string{
			"/api/admin/competitions?limit=0",
			"/api/admin/competitions?limit=101",
			"/api/admin/competitions?offset=-1",
			"/api/admin/competitions?limit=1&limit=2",
		} {
			service := &stubEndpointAdminCompetitionsListService{}
			handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
				slog.New(slog.NewJSONHandler(io.Discard, nil)))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, target))
			assertBackendError(t, recorder, http.StatusBadRequest, "invalid_pagination")
			if calls, _, _, _, _ := service.snapshot(); calls != 0 {
				t.Fatalf("%s reached the service %d times", target, calls)
			}
		}
	})

	t.Run("service receives a bounded context deadline", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
		}
		_, _, deadline, hasDeadline, _ := service.snapshot()
		if !hasDeadline {
			t.Fatal("service context has no deadline")
		}
		if remaining := time.Until(deadline); remaining <= 0 || remaining > lookupTimeout {
			t.Fatalf("service deadline %v outside the 5s budget", remaining)
		}
	})

	t.Run("request cancellation reaches the service", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		request := sessionRequest(http.MethodGet, "/api/admin/competitions")
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request.WithContext(ctx))
		if _, _, _, _, ctxErr := service.snapshot(); !errors.Is(ctxErr, context.Canceled) {
			t.Fatalf("service context error %v", ctxErr)
		}
	})

	t.Run("page query is passed through", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{
			result: domain.CompetitionPage{HasMore: true},
		}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions?limit=7&offset=3"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
		}
		calls, page, _, _, _ := service.snapshot()
		if calls != 1 || page != (domain.Page{Limit: 7, Offset: 3}) {
			t.Fatalf("service calls %d page %+v", calls, page)
		}
		var payload struct {
			Pagination struct {
				Limit   int  `json:"limit"`
				Offset  int  `json:"offset"`
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
		}
		if payload.Pagination.Limit != 7 || payload.Pagination.Offset != 3 || !payload.Pagination.HasMore {
			t.Fatalf("pagination %+v", payload.Pagination)
		}
	})

	t.Run("empty page serializes data as empty array", func(t *testing.T) {
		service := &stubEndpointAdminCompetitionsListService{}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions"))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
		}
		if !bytes.Contains(recorder.Body.Bytes(), []byte(`"data":[]`)) {
			t.Fatalf("empty page body %s", recorder.Body.String())
		}
	})

	t.Run("service failure is sanitized", func(t *testing.T) {
		var logs bytes.Buffer
		service := &stubEndpointAdminCompetitionsListService{
			err: errors.New("pgx: password authentication failed for user caus"),
		}
		handler := endpointAdminCompetitionsListHandler(endpointAdminCompetitionsListAuth(t, "admin"), service,
			slog.New(slog.NewJSONHandler(&logs, nil)))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/competitions"))
		assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
		if bytes.Contains(recorder.Body.Bytes(), []byte("password")) {
			t.Fatalf("body leaked backend detail: %s", recorder.Body.String())
		}
		if bytes.Contains(logs.Bytes(), []byte("password")) {
			t.Fatalf("logs leaked backend detail: %s", logs.String())
		}
		if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
			t.Fatalf("logs missing error type: %s", logs.String())
		}
		if calls, _, _, _, _ := service.snapshot(); calls != 1 {
			t.Fatalf("service calls %d", calls)
		}
	})
}

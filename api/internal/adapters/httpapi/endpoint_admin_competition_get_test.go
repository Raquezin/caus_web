package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type adminCompetitionGetService struct {
	BackendService
	detail    domain.CompetitionDetail
	err       error
	calls     int
	gotID     uuid.UUID
	gotCtx    context.Context
	remaining time.Duration
}

func (s *adminCompetitionGetService) GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error) {
	s.calls++
	s.gotID = id
	s.gotCtx = ctx
	if deadline, ok := ctx.Deadline(); ok {
		s.remaining = time.Until(deadline)
	}
	return s.detail, s.err
}

type adminCompetitionGetContextKey struct{}

func adminCompetitionGetAuth(t *testing.T, account domain.Account) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
}

func adminCompetitionGetMux(service BackendService, auth *Auth, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/admin/competitions/{id}", auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(NewBackend(service, auth, log).adminGetCompetition),
	})))
	return mux
}

func adminCompetitionGetDo(t *testing.T, handler http.Handler, ctx context.Context, method, target string, cookie bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil).WithContext(ctx)
	if cookie {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func adminCompetitionGetAdmin() domain.Account {
	return domain.Account{ID: uuid.New(), FirstName: "Grace", LastName: "Hopper", Role: "admin"}
}

func adminCompetitionGetDetail() domain.CompetitionDetail {
	competitionID := uuid.MustParse("6b1d3f4e-2a4c-4f7d-8c17-9d2b0f6a3e51")
	created := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	return domain.CompetitionDetail{
		Competition: domain.Competition{
			ID:          competitionID,
			Name:        "Spring",
			Description: "Robotics",
			Location:    "Hall A",
			Date:        time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC),
			TeamSize:    3,
			CreatedAt:   created,
			UpdatedAt:   created.Add(time.Hour),
		},
		Teams: []domain.Team{{
			ID:            uuid.MustParse("f0d7f1b4-5f2b-4b6a-9f6e-5c1a2b3d4e5f"),
			CompetitionID: competitionID,
			Name:          adminCompetitionGetPtr("Rocket"),
			Position:      adminCompetitionGetPtr(1),
			Members:       []domain.Person{{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}},
			CreatedAt:     created,
			UpdatedAt:     created.Add(time.Hour),
		}},
	}
}

func adminCompetitionGetPtr[T any](value T) *T {
	return &value
}

func TestEndpointAdminCompetitionGet(t *testing.T) {
	detail := adminCompetitionGetDetail()
	admin := adminCompetitionGetAdmin()
	service := &adminCompetitionGetService{detail: detail}
	handler := adminCompetitionGetMux(service, adminCompetitionGetAuth(t, admin), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	target := "/api/admin/competitions/" + detail.Competition.ID.String()

	ctx := context.WithValue(context.Background(), adminCompetitionGetContextKey{}, "forwarded")
	recorder := adminCompetitionGetDo(t, handler, ctx, http.MethodGet, target, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	var body struct {
		Competition map[string]any   `json:"competition"`
		Teams       []map[string]any `json:"teams"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	if body.Competition["id"] != detail.Competition.ID.String() || body.Competition["name"] != "Spring" ||
		body.Competition["description"] != "Robotics" || body.Competition["location"] != "Hall A" ||
		body.Competition["date"] != "2026-05-02T18:00:00" || body.Competition["team_size"] != float64(3) {
		t.Fatalf("competition payload %v", body.Competition)
	}
	if _, ok := body.Competition["published"]; ok {
		t.Fatalf("competition exposes published: %v", body.Competition)
	}
	if len(body.Teams) != 1 || body.Teams[0]["name"] != "Rocket" || body.Teams[0]["position"] != float64(1) {
		t.Fatalf("teams payload %v", body.Teams)
	}
	members, ok := body.Teams[0]["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members payload %v", body.Teams[0]["members"])
	}
	member := members[0].(map[string]any)
	if len(member) != 3 || member["first_name"] != "Ada" || member["last_name"] != "Lovelace" {
		t.Fatalf("member keys %v", member)
	}
	for _, leaked := range []string{"email", "google_sub", "session", "hash", "password"} {
		if strings.Contains(recorder.Body.String(), leaked) {
			t.Fatalf("body leaks %q: %s", leaked, recorder.Body.String())
		}
	}
	if service.calls != 1 || service.gotID != detail.Competition.ID {
		t.Fatalf("service call id=%v calls=%d", service.gotID, service.calls)
	}
	if service.gotCtx == nil || service.gotCtx.Value(adminCompetitionGetContextKey{}) != "forwarded" {
		t.Fatal("request context was not forwarded to the service")
	}
	if service.remaining <= 0 || service.remaining > 5*time.Second {
		t.Fatalf("service deadline remaining %v", service.remaining)
	}
	if err := service.gotCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("service context error after handler returned: %v", err)
	}

	noCookie := adminCompetitionGetDo(t, handler, context.Background(), http.MethodGet, target, false)
	if noCookie.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d: %s", noCookie.Code, noCookie.Body.String())
	}
	if code := errorCode(t, noCookie); code != "unauthenticated" {
		t.Fatalf("unauthenticated code %q", code)
	}
	if service.calls != 1 {
		t.Fatalf("unauthenticated request reached the service: %d calls", service.calls)
	}

	forbiddenService := &adminCompetitionGetService{detail: detail}
	forbidden := adminCompetitionGetMux(forbiddenService, adminCompetitionGetAuth(t, domain.Account{ID: uuid.New(), Role: "member"}),
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	recorder = adminCompetitionGetDo(t, forbidden, context.Background(), http.MethodGet, target, true)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("member status %d: %s", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "forbidden" {
		t.Fatalf("member code %q", code)
	}
	if forbiddenService.calls != 0 {
		t.Fatalf("member request reached the service: %d calls", forbiddenService.calls)
	}

	for _, invalid := range []string{"/api/admin/competitions/not-a-uuid", "/api/admin/competitions/" + uuid.Nil.String()} {
		invalidService := &adminCompetitionGetService{}
		invalidHandler := adminCompetitionGetMux(invalidService, adminCompetitionGetAuth(t, admin),
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		recorder = adminCompetitionGetDo(t, invalidHandler, context.Background(), http.MethodGet, invalid, true)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d: %s", invalid, recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "invalid_id" {
			t.Fatalf("%s: code %q", invalid, code)
		}
		if invalidService.calls != 0 {
			t.Fatalf("%s: service called %d times", invalid, invalidService.calls)
		}
	}

	missing := &adminCompetitionGetService{err: fmt.Errorf("load: %w", domain.ErrNotFound)}
	missingHandler := adminCompetitionGetMux(missing, adminCompetitionGetAuth(t, admin), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	recorder = adminCompetitionGetDo(t, missingHandler, context.Background(), http.MethodGet, target, true)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("missing status %d: %s", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "not_found" {
		t.Fatalf("missing code %q", code)
	}
	if missing.calls != 1 {
		t.Fatalf("missing request calls %d", missing.calls)
	}

	var logs bytes.Buffer
	failing := &adminCompetitionGetService{err: errors.New("database password rejected")}
	failingHandler := adminCompetitionGetMux(failing, adminCompetitionGetAuth(t, admin), slog.New(slog.NewJSONHandler(&logs, nil)))
	recorder = adminCompetitionGetDo(t, failingHandler, context.Background(), http.MethodGet, target, true)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failure status %d: %s", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "internal_error" {
		t.Fatalf("failure code %q", code)
	}
	if strings.Contains(recorder.Body.String(), "password") || strings.Contains(recorder.Body.String(), "database") {
		t.Fatalf("failure body leaked error: %s", recorder.Body.String())
	}
	if strings.Contains(logs.String(), "password") {
		t.Fatalf("logs leaked error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}

	wrong := adminCompetitionGetDo(t, handler, context.Background(), http.MethodPost, target, true)
	if wrong.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d: %s", wrong.Code, wrong.Body.String())
	}
	if allow := wrong.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("POST Allow %q", allow)
	}
	if service.calls != 1 {
		t.Fatalf("POST reached the service: %d calls", service.calls)
	}
}

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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointTeamGetService struct {
	BackendService
	team         domain.Team
	err          error
	calls        int
	id           uuid.UUID
	deadline     time.Duration
	deadlineAt   time.Time
	hasDeadline  bool
	accountInCtx bool
	ctxErr       error
	doneNonNil   bool
}

func (s *endpointTeamGetService) GetTeam(ctx context.Context, id uuid.UUID) (domain.Team, error) {
	s.calls++
	s.id = id
	s.ctxErr = ctx.Err()
	s.doneNonNil = ctx.Done() != nil
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadlineAt = deadline
		s.deadline = time.Until(deadline)
	}
	_, s.accountInCtx = accountFromContext(ctx)
	return s.team, s.err
}

func endpointTeamGetHandler(auth *Auth, service *endpointTeamGetService, logger *slog.Logger) http.Handler {
	backend := NewBackend(service, auth, logger)
	mux := http.NewServeMux()
	mux.Handle("/api/admin/teams/{id}", auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.getTeam),
	})))
	return mux
}

func endpointTeamGetDo(t *testing.T, handler http.Handler, ctx context.Context, target string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if ctx != nil {
		request = request.WithContext(ctx)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func endpointTeamGetAuth(t *testing.T, role string) *Auth {
	t.Helper()
	return newStubBackendAuth(t,
		&stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: role}}, "https://example.test")
}

func endpointTeamGetSession() []*http.Cookie {
	return []*http.Cookie{{Name: "__Host-caus_session", Value: "session-token"}}
}

var endpointTeamGetLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

func endpointTeamGetTeam() domain.Team {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	name := "Team Alpha"
	photo := "https://example.test/photo.png"
	position := 3
	return domain.Team{
		ID:            uuid.MustParse("7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f"),
		CompetitionID: uuid.MustParse("1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"),
		Name:          &name,
		PhotoURL:      &photo,
		Position:      &position,
		Members: []domain.Person{{
			ID:        uuid.MustParse("9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b"),
			FirstName: "Ada",
			LastName:  "Lovelace",
		}},
		CreatedAt: created,
		UpdatedAt: created.Add(24 * time.Hour),
	}
}

func endpointTeamGetRequireOK(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestEndpointTeamGetReturnsTeam(t *testing.T) {
	service := &endpointTeamGetService{team: endpointTeamGetTeam()}
	auth := endpointTeamGetAuth(t, "admin")
	handler := endpointTeamGetHandler(auth, service, endpointTeamGetLogger)
	recorder := endpointTeamGetDo(t, handler, nil, "/api/admin/teams/"+service.team.ID.String(), endpointTeamGetSession())
	payload := endpointTeamGetRequireOK(t, recorder)
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	members, ok := payload["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members %v", payload["members"])
	}
	wantMember := map[string]any{
		"id":         "9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b",
		"first_name": "Ada",
		"last_name":  "Lovelace",
	}
	if !reflect.DeepEqual(members[0], wantMember) {
		t.Fatalf("member %v, want %v", members[0], wantMember)
	}
	delete(payload, "members")
	want := map[string]any{
		"id":             "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f",
		"competition_id": "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
		"name":           "Team Alpha",
		"photo_url":      "https://example.test/photo.png",
		"position":       float64(3),
		"created_at":     "2026-01-02T03:04:05Z",
		"updated_at":     "2026-01-03T03:04:05Z",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload %v, want %v", payload, want)
	}
	for _, forbidden := range []string{"email", "google_sub", "google-sub", "phone", "token", "session"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("body exposes %q: %s", forbidden, recorder.Body.String())
		}
	}
	if service.calls != 1 {
		t.Fatalf("GetTeam calls %d", service.calls)
	}
	if service.id != service.team.ID {
		t.Fatalf("forwarded id %s, want %s", service.id, service.team.ID)
	}
	if !service.accountInCtx {
		t.Fatal("authenticated account missing from service context")
	}
	if !service.hasDeadline || service.deadline <= 0 || service.deadline > 5*time.Second {
		t.Fatalf("service deadline %v (present %v), want at most %v", service.deadline, service.hasDeadline, 5*time.Second)
	}
	if !service.doneNonNil {
		t.Fatal("service context has no cancellation channel")
	}
	if service.ctxErr != nil {
		t.Fatalf("service context already failed: %v", service.ctxErr)
	}
}

func TestEndpointTeamGetReturnsNullFields(t *testing.T) {
	service := &endpointTeamGetService{team: domain.Team{ID: uuid.New(), Members: []domain.Person{}}}
	handler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)

	recorder := endpointTeamGetDo(t, handler, nil, "/api/admin/teams/"+service.team.ID.String(), endpointTeamGetSession())
	endpointTeamGetRequireOK(t, recorder)
	body := recorder.Body.String()
	for _, fragment := range []string{`"name":null`, `"photo_url":null`, `"position":null`, `"members":[]`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("body missing %s: %s", fragment, body)
		}
	}
}

func TestEndpointTeamGetMissing(t *testing.T) {
	for name, lookupErr := range map[string]error{
		"not found":         domain.ErrNotFound,
		"wrapped not found": fmt.Errorf("lookup: %w", domain.ErrNotFound),
	} {
		t.Run(name, func(t *testing.T) {
			service := &endpointTeamGetService{err: lookupErr}
			handler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)

			recorder := endpointTeamGetDo(t, handler, nil, "/api/admin/teams/"+uuid.NewString(), endpointTeamGetSession())
			assertBackendError(t, recorder, http.StatusNotFound, "not_found")
			if service.calls != 1 {
				t.Fatalf("GetTeam calls %d", service.calls)
			}
		})
	}
}

func TestEndpointTeamGetInvalidID(t *testing.T) {
	service := &endpointTeamGetService{team: endpointTeamGetTeam()}
	handler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)

	for _, target := range []string{
		"/api/admin/teams/not-a-uuid",
		"/api/admin/teams/00000000-0000-0000-0000-000000000000",
	} {
		recorder := endpointTeamGetDo(t, handler, nil, target, endpointTeamGetSession())
		if code := errorCode(t, recorder); recorder.Code != http.StatusBadRequest || code != "invalid_id" {
			t.Fatalf("%s status %d code %q (%s)", target, recorder.Code, code, recorder.Body.String())
		}
	}
	if service.calls != 0 {
		t.Fatalf("invalid ids reached GetTeam %d times", service.calls)
	}
}

func TestEndpointTeamGetRequiresAdmin(t *testing.T) {
	service := &endpointTeamGetService{team: endpointTeamGetTeam()}
	target := "/api/admin/teams/" + service.team.ID.String()
	adminHandler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)
	memberHandler := endpointTeamGetHandler(endpointTeamGetAuth(t, "member"), service, endpointTeamGetLogger)

	missing := endpointTeamGetDo(t, adminHandler, nil, target, nil)
	assertBackendError(t, missing, http.StatusUnauthorized, "unauthenticated")
	member := endpointTeamGetDo(t, memberHandler, nil, target, endpointTeamGetSession())
	assertBackendError(t, member, http.StatusForbidden, "forbidden")
	if service.calls != 0 {
		t.Fatalf("guarded requests reached GetTeam %d times", service.calls)
	}
	if strings.Contains(missing.Body.String()+member.Body.String(), "Team Alpha") {
		t.Fatalf("guarded responses leaked team data: %s %s", missing.Body.String(), member.Body.String())
	}
}

func TestEndpointTeamGetSanitizesFailure(t *testing.T) {
	var logs bytes.Buffer
	auth := endpointTeamGetAuth(t, "admin")
	service := &endpointTeamGetService{err: errors.New("sensitive database detail")}
	handler := endpointTeamGetHandler(auth, service, slog.New(slog.NewJSONHandler(&logs, nil)))

	recorder := endpointTeamGetDo(t, handler, nil, "/api/admin/teams/"+uuid.NewString(), endpointTeamGetSession())
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if body := recorder.Body.String(); strings.Contains(body, "sensitive") || strings.Contains(body, "database detail") {
		t.Fatalf("body leaked failure detail: %s", body)
	}
	if strings.Contains(logs.String(), "sensitive") || strings.Contains(logs.String(), "database detail") {
		t.Fatalf("logs leaked failure detail: %s", logs.String())
	}
}

func TestEndpointTeamGetForwardsCancellation(t *testing.T) {
	service := &endpointTeamGetService{team: domain.Team{ID: uuid.New(), Members: []domain.Person{}}}
	handler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := endpointTeamGetDo(t, handler, cancelled, "/api/admin/teams/"+service.team.ID.String(), endpointTeamGetSession())
	if service.calls != 1 {
		t.Fatalf("GetTeam calls %d", service.calls)
	}
	if !errors.Is(service.ctxErr, context.Canceled) {
		t.Fatalf("service observed context error %v, want context.Canceled", service.ctxErr)
	}
	endpointTeamGetRequireOK(t, recorder)
}

func TestEndpointTeamGetRespectsParentDeadline(t *testing.T) {
	service := &endpointTeamGetService{team: domain.Team{ID: uuid.New(), Members: []domain.Person{}}}
	handler := endpointTeamGetHandler(endpointTeamGetAuth(t, "admin"), service, endpointTeamGetLogger)

	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}
	recorder := endpointTeamGetDo(t, handler, parent, "/api/admin/teams/"+service.team.ID.String(), endpointTeamGetSession())
	endpointTeamGetRequireOK(t, recorder)
	if service.calls != 1 {
		t.Fatalf("GetTeam calls %d", service.calls)
	}
	if !service.hasDeadline {
		t.Fatal("service context has no deadline")
	}
	if service.deadlineAt.After(parentDeadline) {
		t.Fatalf("service deadline %v later than parent deadline %v", service.deadlineAt, parentDeadline)
	}
}

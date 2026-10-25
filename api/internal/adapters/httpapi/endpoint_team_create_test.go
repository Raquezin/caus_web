package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

type endpointTeamCreateService struct {
	BackendService
	team          domain.Team
	err           error
	calls         int
	competitionID uuid.UUID
	input         domain.TeamInput
	hasDeadline   bool
	deadline      time.Duration
}

func (s *endpointTeamCreateService) CreateTeam(ctx context.Context, competitionID uuid.UUID, input domain.TeamInput) (domain.Team, error) {
	s.calls++
	s.competitionID = competitionID
	s.input = input
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	return s.team, s.err
}

func endpointTeamCreateAuth(t *testing.T, account domain.Account, authErr error) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{account: account, err: authErr}, "https://example.test")
}

func endpointTeamCreateHandler(service BackendService, auth *Auth, logs *bytes.Buffer) http.Handler {
	var logger *slog.Logger
	if logs == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	} else {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	backend := NewBackend(service, auth, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/admin/competitions/{id}/teams", backend.createTeam)
	return auth.requireAdmin(auth.requireSameOrigin(mux))
}

func endpointTeamCreateDo(handler http.Handler, competitionID, body string, contentType string,
	cookies []*http.Cookie, origins []string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func endpointTeamCreateSession() []*http.Cookie {
	return []*http.Cookie{{Name: "__Host-caus_session", Value: "session-token"}}
}

func endpointTeamCreateTeam() domain.Team {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return domain.Team{
		ID:            uuid.MustParse("7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f"),
		CompetitionID: uuid.MustParse("1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"),
		Members: []domain.Person{{
			ID:        uuid.MustParse("9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b"),
			FirstName: "Ada",
			LastName:  "Lovelace",
		}},
		CreatedAt: created,
		UpdatedAt: created.Add(24 * time.Hour),
	}
}

func endpointTeamCreatePayload(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestEndpointTeamCreateIndividualUnnamed(t *testing.T) {
	service := &endpointTeamCreateService{team: endpointTeamCreateTeam()}
	auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	handler := endpointTeamCreateHandler(service, auth, nil)
	competitionID := uuid.MustParse("1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d")
	memberID := uuid.MustParse("9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b")

	recorder := endpointTeamCreateDo(handler, competitionID.String(),
		`{"member_ids":["`+memberID.String()+`"]}`, "application/json",
		endpointTeamCreateSession(), []string{"https://example.test"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if location := recorder.Header().Get("Location"); location != "/api/admin/teams/"+service.team.ID.String() {
		t.Fatalf("Location %q", location)
	}
	if service.calls != 1 || service.competitionID != competitionID {
		t.Fatalf("calls %d competition %s", service.calls, service.competitionID)
	}
	if service.input.Name != nil || service.input.PhotoURL != nil || service.input.Position != nil {
		t.Fatalf("nullable input %+v", service.input)
	}
	if len(service.input.MemberIDs) != 1 || service.input.MemberIDs[0] != memberID {
		t.Fatalf("member ids %v", service.input.MemberIDs)
	}
	if !service.hasDeadline || service.deadline <= 0 || service.deadline > lookupTimeout {
		t.Fatalf("service deadline %v (present %v)", service.deadline, service.hasDeadline)
	}
	payload := endpointTeamCreatePayload(t, recorder)
	if payload["id"] != service.team.ID.String() || payload["competition_id"] != service.team.CompetitionID.String() {
		t.Fatalf("identity %v %v", payload["id"], payload["competition_id"])
	}
	if payload["name"] != nil || payload["photo_url"] != nil || payload["position"] != nil {
		t.Fatalf("nullable fields %v %v %v", payload["name"], payload["photo_url"], payload["position"])
	}
	if payload["created_at"] != "2026-01-02T03:04:05Z" || payload["updated_at"] != "2026-01-03T03:04:05Z" {
		t.Fatalf("timestamps %v %v", payload["created_at"], payload["updated_at"])
	}
	members, ok := payload["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members %v", payload["members"])
	}
}

func TestEndpointTeamCreateFullTeam(t *testing.T) {
	service := &endpointTeamCreateService{team: endpointTeamCreateTeam()}
	auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	handler := endpointTeamCreateHandler(service, auth, nil)
	competitionID := uuid.New()
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

	recorder := endpointTeamCreateDo(handler, competitionID.String(),
		`{"name":"Team A","photo_url":"https://example.com/p.png","position":2,"member_ids":["`+
			id1.String()+`","`+id2.String()+`","`+id3.String()+`"]}`, "application/json",
		endpointTeamCreateSession(), []string{"https://example.test"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 {
		t.Fatalf("calls %d", service.calls)
	}
	if service.input.Name == nil || *service.input.Name != "Team A" ||
		service.input.PhotoURL == nil || *service.input.PhotoURL != "https://example.com/p.png" ||
		service.input.Position == nil || *service.input.Position != 2 {
		t.Fatalf("input %+v", service.input)
	}
	if len(service.input.MemberIDs) != 3 || service.input.MemberIDs[0] != id1 ||
		service.input.MemberIDs[1] != id2 || service.input.MemberIDs[2] != id3 {
		t.Fatalf("member ids %v", service.input.MemberIDs)
	}
}

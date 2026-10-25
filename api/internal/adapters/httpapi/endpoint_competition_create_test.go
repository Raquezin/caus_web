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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

const endpointCompetitionCreateBody = `{"name":"Spring CTF","description":"Capture the flag",` +
	`"date":"2026-05-02T18:30:00","location":"Madrid","team_size":4}`

type stubEndpointCompetitionCreateService struct {
	BackendService
	competition domain.Competition
	err         error
	calls       int
	input       domain.CompetitionInput
	deadline    time.Time
	hasDeadline bool
}

func (s *stubEndpointCompetitionCreateService) CreateCompetition(ctx context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	s.calls++
	s.input = input
	s.deadline, s.hasDeadline = ctx.Deadline()
	return s.competition, s.err
}

type stubEndpointCompetitionCreateRepository struct {
	application.BackendRepository
	calls int
	input domain.CompetitionInput
	err   error
}

func (s *stubEndpointCompetitionCreateRepository) CreateCompetition(_ context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	s.calls++
	s.input = input
	if s.err != nil {
		return domain.Competition{}, s.err
	}
	return domain.Competition{
		ID: uuid.New(), Name: input.Name, Description: input.Description,
		Location: input.Location, Date: input.Date, TeamSize: input.TeamSize,
	}, nil
}

type endpointCompetitionCreateApplicationService struct {
	BackendService
	backend *application.Backend
	calls   int
}

func (s *endpointCompetitionCreateApplicationService) CreateCompetition(ctx context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	s.calls++
	return s.backend.CreateCompetition(ctx, input)
}

func endpointCompetitionCreateAuth(t *testing.T, role string) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Email: "ada@example.com", Role: role},
	}, "https://example.test")
}

func endpointCompetitionCreateHandler(auth *Auth, service BackendService, log *slog.Logger) http.Handler {
	backend := NewBackend(service, auth, log)
	return auth.requireAdmin(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodPost: http.HandlerFunc(backend.createCompetition),
	})))
}

func endpointCompetitionCreateRequest(body string) *http.Request {
	request := sessionRequest(http.MethodPost, "/api/admin/competitions")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.test")
	request.Body = io.NopCloser(strings.NewReader(body))
	return request
}

func endpointCompetitionCreateFixture() domain.Competition {
	created := time.Date(2026, 5, 1, 11, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	return domain.Competition{
		ID:          uuid.MustParse("8a1f6d3c-2b4e-4c9a-9f7d-1e2b3c4d5e6f"),
		Name:        "Spring CTF",
		Description: "Capture the flag",
		Date:        time.Date(2026, 5, 2, 18, 30, 0, 0, time.UTC),
		Location:    "Madrid",
		TeamSize:    4,
		CreatedAt:   created,
		UpdatedAt:   created.Add(2 * time.Hour),
	}
}

func endpointCompetitionCreateLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestEndpointCompetitionCreate(t *testing.T) {
	competition := endpointCompetitionCreateFixture()
	service := &stubEndpointCompetitionCreateService{competition: competition}
	handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
		endpointCompetitionCreateLogger())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointCompetitionCreateRequest(endpointCompetitionCreateBody))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	wantLocation := "/api/admin/competitions/" + competition.ID.String()
	if location := recorder.Header().Get("Location"); location != wantLocation {
		t.Fatalf("Location %q, want %q", location, wantLocation)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	if len(payload) != 8 {
		t.Fatalf("competition exposes %d fields: %v", len(payload), payload)
	}
	if payload["id"] != competition.ID.String() || payload["name"] != "Spring CTF" ||
		payload["description"] != "Capture the flag" || payload["date"] != "2026-05-02T18:30:00" ||
		payload["location"] != "Madrid" || payload["team_size"] != float64(4) ||
		payload["created_at"] != "2026-05-01T09:30:00Z" || payload["updated_at"] != "2026-05-01T11:30:00Z" {
		t.Fatalf("competition payload %v", payload)
	}
	if _, ok := payload["published"]; ok {
		t.Fatalf("competition payload exposes published: %v", payload)
	}
	if service.calls != 1 {
		t.Fatalf("service calls %d", service.calls)
	}
	wantInput := domain.CompetitionInput{
		Name: "Spring CTF", Description: "Capture the flag", Location: "Madrid",
		Date: time.Date(2026, 5, 2, 18, 30, 0, 0, time.UTC), TeamSize: 4,
	}
	if service.input != wantInput {
		t.Fatalf("service input %+v, want %+v", service.input, wantInput)
	}
	if !service.hasDeadline {
		t.Fatal("service context has no deadline")
	}
	if remaining := time.Until(service.deadline); remaining <= 0 || remaining > lookupTimeout {
		t.Fatalf("service deadline %v outside the 5s budget", remaining)
	}
}

func TestEndpointCompetitionCreateDefaultsTeamSize(t *testing.T) {
	service := &stubEndpointCompetitionCreateService{competition: endpointCompetitionCreateFixture()}
	handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
		endpointCompetitionCreateLogger())
	body := `{"name":"Spring CTF","description":"Capture the flag","date":"2026-05-02T18:30:00","location":"Madrid"}`
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointCompetitionCreateRequest(body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 || service.input.TeamSize != 3 {
		t.Fatalf("service calls %d team size %d", service.calls, service.input.TeamSize)
	}
}

func TestEndpointCompetitionCreateRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name  string
		field string
		body  string
	}{
		{"blank name", "name", `{"name":"  ","description":"D","date":"2026-05-02T18:30:00","location":"H"}`},
		{"missing name", "name", `{"description":"D","date":"2026-05-02T18:30:00","location":"H"}`},
		{"missing date", "date", `{"name":"N","description":"D","location":"H"}`},
		{"invalid date", "date", `{"name":"N","description":"D","date":"2026-02-30T18:30:00","location":"H"}`},
		{"blank description", "description", `{"name":"N","description":" ","date":"2026-05-02T18:30:00","location":"H"}`},
		{"blank location", "location", `{"name":"N","description":"D","date":"2026-05-02T18:30:00","location":""}`},
		{"zero team size", "team_size", `{"name":"N","description":"D","date":"2026-05-02T18:30:00","location":"H","team_size":0}`},
		{"negative team size", "team_size", `{"name":"N","description":"D","date":"2026-05-02T18:30:00","location":"H","team_size":-1}`},
		{"overflow team size", "team_size", `{"name":"N","description":"D","date":"2026-05-02T18:30:00","location":"H","team_size":32768}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &stubEndpointCompetitionCreateRepository{}
			service := &endpointCompetitionCreateApplicationService{backend: application.NewBackend(repo)}
			handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
				endpointCompetitionCreateLogger())
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, endpointCompetitionCreateRequest(testCase.body))
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
			}
			var payload struct {
				Error struct {
					Code   string            `json:"code"`
					Fields map[string]string `json:"fields"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
			}
			if payload.Error.Code != "validation_failed" || payload.Error.Fields[testCase.field] == "" {
				t.Fatalf("validation payload %+v", payload.Error)
			}
			if repo.calls != 0 {
				t.Fatalf("repository called %d times for invalid input", repo.calls)
			}
		})
	}
}

func TestEndpointCompetitionCreateDuplicate(t *testing.T) {
	service := &stubEndpointCompetitionCreateService{err: domain.ErrConflict}
	handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
		endpointCompetitionCreateLogger())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointCompetitionCreateRequest(endpointCompetitionCreateBody))
	assertBackendError(t, recorder, http.StatusConflict, "conflict")
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("conflict returned Location %q", location)
	}
	if service.calls != 1 {
		t.Fatalf("service calls %d", service.calls)
	}
}

func TestEndpointCompetitionCreateBackendFailure(t *testing.T) {
	var logs bytes.Buffer
	repo := &stubEndpointCompetitionCreateRepository{err: errors.New("password rejected")}
	service := &endpointCompetitionCreateApplicationService{backend: application.NewBackend(repo)}
	handler := endpointCompetitionCreateHandler(endpointCompetitionCreateAuth(t, "admin"), service,
		slog.New(slog.NewJSONHandler(&logs, nil)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, endpointCompetitionCreateRequest(endpointCompetitionCreateBody))
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if bytes.Contains(recorder.Body.Bytes(), []byte("password")) || bytes.Contains(logs.Bytes(), []byte("password")) {
		t.Fatalf("failure leaked detail: body %s logs %s", recorder.Body.String(), logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
	if repo.calls != 1 {
		t.Fatalf("repository calls %d", repo.calls)
	}
}

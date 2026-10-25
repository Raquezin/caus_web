package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubCompetitionGetService struct {
	BackendService
	detail      domain.CompetitionDetail
	err         error
	calls       int
	gotID       uuid.UUID
	hasDeadline bool
	remaining   time.Duration
	contextOK   bool
}

func (s *stubCompetitionGetService) GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error) {
	s.calls++
	s.gotID = id
	deadline, ok := ctx.Deadline()
	s.hasDeadline = ok
	if ok {
		s.remaining = time.Until(deadline)
	}
	s.contextOK = ctx.Err() == nil
	return s.detail, s.err
}

func serveCompetitionGet(service BackendService, id string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/competitions/"+id, nil)
	request.SetPathValue("id", id)
	recorder := httptest.NewRecorder()
	NewBackend(service, nil, nil).getCompetition(recorder, request)
	return recorder
}

func TestEndpointCompetitionGet(t *testing.T) {
	competitionID := uuid.New()
	firstTeamID := uuid.New()
	secondTeamID := uuid.New()
	firstPosition := 1
	secondName := "Team B"
	firstMemberID := uuid.New()
	detail := domain.CompetitionDetail{
		Competition: domain.Competition{
			ID:          competitionID,
			Name:        "Spring",
			Description: "Description",
			Date:        time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC),
			Location:    "Hall",
			TeamSize:    3,
			CreatedAt:   time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		},
		Teams: []domain.Team{
			{
				ID:            firstTeamID,
				CompetitionID: competitionID,
				Position:      &firstPosition,
				Members:       []domain.Person{{ID: firstMemberID, FirstName: "Grace", LastName: "Hopper"}},
			},
			{
				ID:            secondTeamID,
				CompetitionID: competitionID,
				Name:          &secondName,
			},
		},
	}

	t.Run("detail", func(t *testing.T) {
		service := &stubCompetitionGetService{detail: detail}
		recorder := serveCompetitionGet(service, competitionID.String())
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		if service.calls != 1 || service.gotID != competitionID {
			t.Fatalf("service calls %d id %v", service.calls, service.gotID)
		}
		if !service.hasDeadline || !service.contextOK {
			t.Fatalf("service context deadline %v ok %v", service.hasDeadline, service.contextOK)
		}
		if service.remaining > lookupTimeout || lookupTimeout-service.remaining > 250*time.Millisecond {
			t.Fatalf("service context remaining %v, want about %v", service.remaining, lookupTimeout)
		}
		var body struct {
			Competition struct {
				ID       uuid.UUID `json:"id"`
				Name     string    `json:"name"`
				Date     string    `json:"date"`
				TeamSize int       `json:"team_size"`
			} `json:"competition"`
			Teams []struct {
				ID       uuid.UUID `json:"id"`
				Name     *string   `json:"name"`
				Position *int      `json:"position"`
				Members  []struct {
					ID        uuid.UUID `json:"id"`
					FirstName string    `json:"first_name"`
					LastName  string    `json:"last_name"`
				} `json:"members"`
			} `json:"teams"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Competition.ID != competitionID || body.Competition.Name != "Spring" ||
			body.Competition.Date != "2026-05-02T18:00:00" || body.Competition.TeamSize != 3 {
			t.Fatalf("competition %+v", body.Competition)
		}
		if len(body.Teams) != 2 || body.Teams[0].ID != firstTeamID || body.Teams[1].ID != secondTeamID {
			t.Fatalf("teams %+v", body.Teams)
		}
		if body.Teams[0].Position == nil || *body.Teams[0].Position != 1 || body.Teams[0].Name != nil {
			t.Fatalf("first team %+v", body.Teams[0])
		}
		if body.Teams[1].Position != nil || body.Teams[1].Name == nil || *body.Teams[1].Name != secondName {
			t.Fatalf("second team %+v", body.Teams[1])
		}
		if len(body.Teams[0].Members) != 1 || body.Teams[0].Members[0].ID != firstMemberID ||
			body.Teams[0].Members[0].FirstName != "Grace" || body.Teams[0].Members[0].LastName != "Hopper" {
			t.Fatalf("members %+v", body.Teams[0].Members)
		}
		payload := recorder.Body.String()
		for _, forbidden := range []string{"email", "google_sub", "role", "GoogleSub"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("payload exposes %q: %s", forbidden, payload)
			}
		}
	})

	t.Run("empty teams", func(t *testing.T) {
		service := &stubCompetitionGetService{detail: domain.CompetitionDetail{Competition: detail.Competition}}
		recorder := serveCompetitionGet(service, competitionID.String())
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), `"teams":[]`) {
			t.Fatalf("teams not empty array: %s", recorder.Body.String())
		}
	})

	t.Run("invalid uuid", func(t *testing.T) {
		cases := []struct {
			name string
			id   string
		}{
			{name: "malformed", id: "not-a-uuid"},
			{name: "empty", id: ""},
			{name: "nil", id: uuid.Nil.String()},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				service := &stubCompetitionGetService{}
				recorder := serveCompetitionGet(service, testCase.id)
				if recorder.Code != http.StatusBadRequest || service.calls != 0 {
					t.Fatalf("status %d calls %d: %s", recorder.Code, service.calls, recorder.Body.String())
				}
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error.Code != "invalid_id" {
					t.Fatalf("error code %q: %s", body.Error.Code, recorder.Body.String())
				}
			})
		}
	})

	t.Run("not found", func(t *testing.T) {
		service := &stubCompetitionGetService{err: domain.ErrNotFound}
		recorder := serveCompetitionGet(service, competitionID.String())
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("timeout sanitized", func(t *testing.T) {
		service := &stubCompetitionGetService{err: context.DeadlineExceeded}
		recorder := serveCompetitionGet(service, competitionID.String())
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "deadline") {
			t.Fatalf("raw error leaked: %s", recorder.Body.String())
		}
	})

	t.Run("internal error sanitized", func(t *testing.T) {
		service := &stubCompetitionGetService{err: errors.New("database exploded for ada@example.com")}
		recorder := serveCompetitionGet(service, competitionID.String())
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		payload := recorder.Body.String()
		if strings.Contains(payload, "exploded") || strings.Contains(payload, "ada@example.com") {
			t.Fatalf("raw error leaked: %s", payload)
		}
	})
}

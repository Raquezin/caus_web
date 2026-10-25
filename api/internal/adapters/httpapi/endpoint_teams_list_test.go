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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubTeamsListService struct {
	BackendService
	mu            sync.Mutex
	calls         int
	competitionID uuid.UUID
	page          domain.Page
	hasDeadline   bool
	result        domain.TeamPage
	err           error
}

func (s *stubTeamsListService) ListTeams(ctx context.Context, competitionID uuid.UUID, page domain.Page) (domain.TeamPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.competitionID = competitionID
	s.page = page
	_, s.hasDeadline = ctx.Deadline()
	return s.result, s.err
}

func (s *stubTeamsListService) snapshot() (int, uuid.UUID, domain.Page, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.competitionID, s.page, s.hasDeadline
}

func endpointTeamsListServer(t *testing.T, role string, service BackendService) *httptest.Server {
	t.Helper()
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Email: "ada@example.com", Role: role},
	}, "https://example.test")
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.Handle("/api/admin/competitions/{id}/teams", auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.listTeams),
	})))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointTeamsListCall(t *testing.T, server *httptest.Server, target, rawQuery string, session bool) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rawQuery != "" {
		request.URL.RawQuery = rawQuery
	}
	if session {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, body
}

func endpointTeamsListFixture() (uuid.UUID, []domain.Team) {
	competitionID := uuid.MustParse("8a1f6d3c-2b4e-4c9a-9f7d-1e2b3c4d5e6f")
	created := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	name := "Team A"
	photo := "https://photos.test/a"
	position := 2
	team := domain.Team{
		ID:            uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		CompetitionID: competitionID,
		Name:          &name,
		PhotoURL:      &photo,
		Position:      &position,
		Members:       []domain.Person{{ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), FirstName: "Grace", LastName: "Hopper"}},
		CreatedAt:     created,
		UpdatedAt:     created.Add(2 * time.Hour),
	}
	return competitionID, []domain.Team{team, {
		ID:            uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		CompetitionID: competitionID,
		Members:       []domain.Person{{ID: uuid.MustParse("44444444-4444-4444-8444-444444444444"), FirstName: "Ada", LastName: "Lovelace"}},
		CreatedAt:     created,
		UpdatedAt:     created,
	}}
}

func endpointTeamsListKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestEndpointTeamsListAdminResponse(t *testing.T) {
	competitionID, teams := endpointTeamsListFixture()
	service := &stubTeamsListService{result: domain.TeamPage{Teams: teams, HasMore: true}}
	server := endpointTeamsListServer(t, "admin", service)
	target := "/api/admin/competitions/" + competitionID.String() + "/teams"
	status, header, body := endpointTeamsListCall(t, server, target, "limit=2&offset=3", true)
	if status != http.StatusOK {
		t.Fatalf("status %d (%s)", status, body)
	}
	if header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type %q", header.Get("Content-Type"))
	}
	var payload struct {
		Data []struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
		Pagination struct {
			Limit   int  `json:"limit"`
			Offset  int  `json:"offset"`
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if len(payload.Data) != 2 || payload.Data[0].ID != teams[0].ID || payload.Data[1].ID != teams[1].ID {
		t.Fatalf("data order %+v", payload.Data)
	}
	if payload.Pagination.Limit != 2 || payload.Pagination.Offset != 3 || !payload.Pagination.HasMore {
		t.Fatalf("pagination %+v", payload.Pagination)
	}
	calls, gotCompetition, gotPage, hasDeadline := service.snapshot()
	if calls != 1 || gotCompetition != competitionID || gotPage != (domain.Page{Limit: 2, Offset: 3}) || !hasDeadline {
		t.Fatalf("service calls %d competition %v page %+v deadline %v", calls, gotCompetition, gotPage, hasDeadline)
	}
}

func TestEndpointTeamsListPrivacy(t *testing.T) {
	competitionID, teams := endpointTeamsListFixture()
	service := &stubTeamsListService{result: domain.TeamPage{Teams: teams}}
	server := endpointTeamsListServer(t, "admin", service)
	status, _, body := endpointTeamsListCall(t, server, "/api/admin/competitions/"+competitionID.String()+"/teams", "", true)
	if status != http.StatusOK {
		t.Fatalf("status %d (%s)", status, body)
	}
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	wantTeamKeys := []string{"competition_id", "created_at", "id", "members", "name", "photo_url", "position", "updated_at"}
	if got := endpointTeamsListKeys(payload.Data[0]); strings.Join(got, ",") != strings.Join(wantTeamKeys, ",") {
		t.Fatalf("team keys %v", got)
	}
	members, ok := payload.Data[0]["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members %v", payload.Data[0]["members"])
	}
	member, ok := members[0].(map[string]any)
	if !ok || strings.Join(endpointTeamsListKeys(member), ",") != "first_name,id,last_name" {
		t.Fatalf("member keys %v", member)
	}
	created, ok := payload.Data[0]["created_at"].(string)
	if !ok {
		t.Fatalf("created_at %v", payload.Data[0]["created_at"])
	}
	if parsed, err := time.Parse(time.RFC3339, created); err != nil || !strings.HasSuffix(created, "Z") {
		t.Fatalf("created_at %q: %v", created, err)
	} else if !parsed.Equal(teams[0].CreatedAt) {
		t.Fatalf("created_at %v", parsed)
	}
	if updated, ok := payload.Data[0]["updated_at"].(string); !ok || !strings.HasSuffix(updated, "Z") {
		t.Fatalf("updated_at %v", payload.Data[0]["updated_at"])
	}
	for _, forbidden := range []string{"email", "google_sub", "@example.com"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("body leaks %q: %s", forbidden, body)
		}
	}
}

func TestEndpointTeamsListEmptyAndErrors(t *testing.T) {
	competitionID := uuid.New()
	target := "/api/admin/competitions/" + competitionID.String() + "/teams"

	t.Run("empty data is array", func(t *testing.T) {
		service := &stubTeamsListService{result: domain.TeamPage{Teams: []domain.Team{}}}
		status, _, body := endpointTeamsListCall(t, endpointTeamsListServer(t, "admin", service), target, "", true)
		if status != http.StatusOK || !bytes.Contains(body, []byte(`"data":[]`)) {
			t.Fatalf("status %d body %s", status, body)
		}
		var payload struct {
			Pagination struct {
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Pagination.HasMore {
			t.Fatalf("pagination has_more true or decode error %v: %s", err, body)
		}
	})

	t.Run("missing parent is 404", func(t *testing.T) {
		service := &stubTeamsListService{err: domain.ErrNotFound}
		status, _, body := endpointTeamsListCall(t, endpointTeamsListServer(t, "admin", service), target, "", true)
		if status != http.StatusNotFound || !bytes.Contains(body, []byte(`"not_found"`)) {
			t.Fatalf("status %d body %s", status, body)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		service := &stubTeamsListService{}
		status, _, body := endpointTeamsListCall(t, endpointTeamsListServer(t, "admin", service),
			"/api/admin/competitions/not-a-uuid/teams", "", true)
		if status != http.StatusBadRequest || !bytes.Contains(body, []byte(`"invalid_id"`)) {
			t.Fatalf("status %d body %s", status, body)
		}
		if calls, _, _, _ := service.snapshot(); calls != 0 {
			t.Fatalf("service calls %d", calls)
		}
	})

	t.Run("invalid pagination", func(t *testing.T) {
		for _, rawQuery := range []string{"limit=0", "limit=101", "offset=-1", "limit=1&limit=2", "limit=abc", "limit=%zz"} {
			service := &stubTeamsListService{}
			status, _, body := endpointTeamsListCall(t, endpointTeamsListServer(t, "admin", service), target, rawQuery, true)
			if status != http.StatusBadRequest || !bytes.Contains(body, []byte(`"invalid_pagination"`)) {
				t.Fatalf("%s: status %d body %s", rawQuery, status, body)
			}
			if calls, _, _, _ := service.snapshot(); calls != 0 {
				t.Fatalf("%s: service calls %d", rawQuery, calls)
			}
		}
	})

	t.Run("auth", func(t *testing.T) {
		service := &stubTeamsListService{}
		server := endpointTeamsListServer(t, "admin", service)
		status, _, body := endpointTeamsListCall(t, server, target, "", false)
		if status != http.StatusUnauthorized || !bytes.Contains(body, []byte(`"unauthenticated"`)) {
			t.Fatalf("no cookie: status %d body %s", status, body)
		}
		if calls, _, _, _ := service.snapshot(); calls != 0 {
			t.Fatalf("sessionless request reached service %d times", calls)
		}
		member := &stubTeamsListService{}
		status, _, body = endpointTeamsListCall(t, endpointTeamsListServer(t, "member", member), target, "", true)
		if status != http.StatusForbidden || !bytes.Contains(body, []byte(`"forbidden"`)) {
			t.Fatalf("member: status %d body %s", status, body)
		}
		if calls, _, _, _ := member.snapshot(); calls != 0 {
			t.Fatalf("member reached service %d times", calls)
		}
		if status, _, body = endpointTeamsListCall(t, server, target, "", true); status != http.StatusOK {
			t.Fatalf("admin: status %d body %s", status, body)
		}
		if calls, _, _, _ := service.snapshot(); calls != 1 {
			t.Fatalf("admin reached service %d times", calls)
		}
	})

	t.Run("error sanitized", func(t *testing.T) {
		service := &stubTeamsListService{err: errors.New("boom priya-secret-token")}
		status, _, body := endpointTeamsListCall(t, endpointTeamsListServer(t, "admin", service), target, "", true)
		if status != http.StatusInternalServerError || !bytes.Contains(body, []byte(`"internal_error"`)) {
			t.Fatalf("status %d body %s", status, body)
		}
		for _, forbidden := range []string{"priya-secret-token", "internal error stack", "SELECT", "panic"} {
			if bytes.Contains(body, []byte(forbidden)) {
				t.Fatalf("body leaked %q: %s", forbidden, body)
			}
		}
	})
}

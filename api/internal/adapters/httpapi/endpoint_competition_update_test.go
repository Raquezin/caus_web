package httpapi

import (
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

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

type endpointCompetitionUpdateRepo struct {
	application.BackendRepository
	competition domain.Competition
	err         error
	calls       int
	id          uuid.UUID
	patch       domain.CompetitionPatch
	hasDeadline bool
}

func (r *endpointCompetitionUpdateRepo) UpdateCompetition(ctx context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error) {
	r.calls++
	r.id = id
	r.patch = patch
	_, r.hasDeadline = ctx.Deadline()
	return r.competition, r.err
}

type endpointCompetitionUpdateService struct {
	BackendService
	repo *endpointCompetitionUpdateRepo
}

func (s *endpointCompetitionUpdateService) UpdateCompetition(ctx context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error) {
	return application.NewBackend(s.repo).UpdateCompetition(ctx, id, patch)
}

func endpointCompetitionUpdateAuth(t *testing.T, role string) *Auth {
	t.Helper()
	account := domain.Account{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Role: role}
	return newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
}

func endpointCompetitionUpdateFixture() domain.Competition {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return domain.Competition{
		ID:          uuid.MustParse("5d0d1f47-b3c9-4a38-9b3e-0f6f2c1a7e11"),
		Name:        "Autumn",
		Description: "Description",
		Location:    "Hall",
		Date:        time.Date(2026, 7, 18, 19, 30, 0, 0, time.UTC),
		TeamSize:    4,
		CreatedAt:   created,
		UpdatedAt:   created.Add(time.Hour),
	}
}

func newEndpointCompetitionUpdateServer(t *testing.T, auth *Auth, repo *endpointCompetitionUpdateRepo, log *slog.Logger) *httptest.Server {
	t.Helper()
	backend := NewBackend(&endpointCompetitionUpdateService{repo: repo}, auth, log)
	mux := http.NewServeMux()
	mux.Handle("/api/admin/competitions/{id}", auth.requireAdmin(auth.requireSameOrigin(endpointMethods(
		map[string]http.Handler{http.MethodPatch: http.HandlerFunc(backend.updateCompetition)},
	))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointCompetitionUpdateTarget(id uuid.UUID) string {
	return "/api/admin/competitions/" + id.String()
}

func endpointCompetitionUpdateDo(t *testing.T, server *httptest.Server, method, target, origin, contentType, body string,
	cookies []*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, data
}

func endpointCompetitionUpdateSession() []*http.Cookie {
	return []*http.Cookie{{Name: "__Host-caus_session", Value: "session-token"}}
}

func TestEndpointCompetitionUpdateAppliesPartialPatch(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))

	response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch,
		endpointCompetitionUpdateTarget(repo.competition.ID), "https://example.test", "application/json",
		`{"team_size":4,"date":"2026-07-18T19:30:00"}`, endpointCompetitionUpdateSession())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if repo.calls != 1 || repo.id != repo.competition.ID || !repo.hasDeadline {
		t.Fatalf("service call %d id %v deadline %v", repo.calls, repo.id, repo.hasDeadline)
	}
	if repo.patch.TeamSize == nil || *repo.patch.TeamSize != 4 || repo.patch.Date == nil ||
		!repo.patch.Date.Equal(repo.competition.Date) {
		t.Fatalf("forwarded patch %+v", repo.patch)
	}
	if repo.patch.Name != nil || repo.patch.Description != nil || repo.patch.Location != nil {
		t.Fatalf("handler filled omitted fields: %+v", repo.patch)
	}

	var payload struct {
		ID        uuid.UUID `json:"id"`
		Name      string    `json:"name"`
		Date      string    `json:"date"`
		TeamSize  int       `json:"team_size"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	if payload.ID != repo.competition.ID || payload.Name != "Autumn" || payload.Date != "2026-07-18T19:30:00" ||
		payload.TeamSize != 4 {
		t.Fatalf("payload %+v", payload)
	}
	if !payload.CreatedAt.Equal(repo.competition.CreatedAt) || !payload.UpdatedAt.Equal(repo.competition.UpdatedAt) {
		t.Fatalf("timestamps %+v", payload)
	}
	if strings.Contains(string(body), "google_sub") {
		t.Fatalf("response exposed private fields: %s", body)
	}
}

func TestEndpointCompetitionUpdateRequiresAdminSession(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	target := endpointCompetitionUpdateTarget(repo.competition.ID)

	response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch, target, "https://example.test",
		"application/json", `{"name":"Autumn"}`, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if code := endpointCompetitionUpdateErrorCode(t, body); code != "unauthenticated" {
		t.Fatalf("code %q", code)
	}
	if repo.calls != 0 {
		t.Fatalf("unauthenticated request reached the service %d times", repo.calls)
	}

	member := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "member"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	response, body = endpointCompetitionUpdateDo(t, member, http.MethodPatch, target, "https://example.test",
		"application/json", `{"name":"Autumn"}`, endpointCompetitionUpdateSession())
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("member status %d (%s)", response.StatusCode, body)
	}
	if code := endpointCompetitionUpdateErrorCode(t, body); code != "forbidden" {
		t.Fatalf("member code %q", code)
	}
	if repo.calls != 0 {
		t.Fatalf("member request reached the service %d times", repo.calls)
	}
}

func TestEndpointCompetitionUpdateRequiresSameOrigin(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	auth := endpointCompetitionUpdateAuth(t, "admin")
	server := newEndpointCompetitionUpdateServer(t, auth, repo, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	target := endpointCompetitionUpdateTarget(repo.competition.ID)

	for _, origin := range []string{"", "https://evil.test", "http://example.test"} {
		response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch, target, origin, "application/json",
			`{"name":"Autumn"}`, endpointCompetitionUpdateSession())
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q status %d (%s)", origin, response.StatusCode, body)
		}
		if code := endpointCompetitionUpdateErrorCode(t, body); code != "origin_denied" {
			t.Fatalf("origin %q code %q", origin, code)
		}
	}
	if repo.calls != 0 {
		t.Fatalf("originless request reached the service %d times", repo.calls)
	}

	request, err := http.NewRequest(http.MethodPatch, server.URL+target, strings.NewReader(`{"name":"Autumn"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Add("Origin", "https://example.test")
	request.Header.Add("Origin", "https://example.test")
	for _, cookie := range endpointCompetitionUpdateSession() {
		request.AddCookie(cookie)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("duplicate origin status %d", response.StatusCode)
	}
}

func TestEndpointCompetitionUpdateRejectsWrongMethod(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	target := endpointCompetitionUpdateTarget(repo.competition.ID)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		response, body := endpointCompetitionUpdateDo(t, server, method, target, "https://example.test", "",
			"", endpointCompetitionUpdateSession())
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status %d (%s)", method, response.StatusCode, body)
		}
		if allow := response.Header.Get("Allow"); allow != "PATCH" {
			t.Fatalf("%s Allow %q", method, allow)
		}
	}
	if repo.calls != 0 {
		t.Fatalf("wrong method reached the service %d times", repo.calls)
	}
}

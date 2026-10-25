package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

type endpointTeamUpdateService struct {
	BackendService
	team         domain.Team
	err          error
	calls        int
	id           uuid.UUID
	patch        domain.TeamPatch
	deadline     time.Duration
	hasDeadline  bool
	accountInCtx bool
}

func (s *endpointTeamUpdateService) UpdateTeam(ctx context.Context, id uuid.UUID, patch domain.TeamPatch) (domain.Team, error) {
	s.calls++
	s.id = id
	s.patch = patch
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	_, s.accountInCtx = accountFromContext(ctx)
	if s.err != nil {
		return domain.Team{}, s.err
	}
	current := domain.TeamInput{
		Name:      s.team.Name,
		PhotoURL:  s.team.PhotoURL,
		Position:  s.team.Position,
		MemberIDs: []uuid.UUID{uuid.MustParse("0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d")},
	}
	merged, err := application.ApplyTeamPatch(current, patch)
	if err != nil {
		return domain.Team{}, err
	}
	team := s.team
	team.Name = merged.Name
	team.PhotoURL = merged.PhotoURL
	team.Position = merged.Position
	return team, nil
}

func newEndpointTeamUpdateServer(t *testing.T, auth *Auth, backend *Backend) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/admin/teams/{id}", auth.requireAdmin(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodPatch: http.HandlerFunc(backend.updateTeam),
	}))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointTeamUpdateDo(t *testing.T, server *httptest.Server, method, target, body string,
	cookies []*http.Cookie, origin string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+target, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
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
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, payload
}

func endpointTeamUpdateAdmin(t *testing.T) (*Auth, *endpointTeamUpdateService) {
	t.Helper()
	service := &endpointTeamUpdateService{}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: "admin"}},
		"https://example.test")
	return auth, service
}

func endpointTeamUpdateSession() *http.Cookie {
	return &http.Cookie{Name: "__Host-caus_session", Value: "session-token"}
}

func endpointTeamUpdateTeam() domain.Team {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	name := "Team A"
	photo := "https://example.test/photo.png"
	position := 2
	memberID := uuid.MustParse("0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d")
	return domain.Team{
		ID:            uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"),
		CompetitionID: uuid.MustParse("4a7b5b4f-ae72-4f9c-b4f2-5b1f2f6d0e32"),
		Name:          &name,
		PhotoURL:      &photo,
		Position:      &position,
		Members:       []domain.Person{{ID: memberID, FirstName: "Ada", LastName: "Lovelace"}},
		CreatedAt:     created,
		UpdatedAt:     created.Add(time.Hour),
	}
}

func endpointTeamUpdateErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return response.Error.Code
}

func TestEndpointTeamUpdateRequiresAdminAndOrigin(t *testing.T) {
	auth, service := endpointTeamUpdateAdmin(t)
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	server := newEndpointTeamUpdateServer(t, auth, backend)
	target := "/api/admin/teams/" + endpointTeamUpdateTeam().ID.String()
	origin := "https://example.test"

	missing, body := endpointTeamUpdateDo(t, server, http.MethodPatch, target, `{"name":"Team B"}`, nil, origin)
	if missing.StatusCode != http.StatusUnauthorized || endpointTeamUpdateErrorCode(t, body) != "unauthenticated" {
		t.Fatalf("missing session: %d (%s)", missing.StatusCode, body)
	}

	memberAuth := newStubBackendAuth(t, &stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: "member"}}, origin)
	member := newEndpointTeamUpdateServer(t, memberAuth,
		NewBackend(service, memberAuth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	forbidden, body := endpointTeamUpdateDo(t, member, http.MethodPatch, target, `{"name":"Team B"}`,
		[]*http.Cookie{endpointTeamUpdateSession()}, origin)
	if forbidden.StatusCode != http.StatusForbidden || endpointTeamUpdateErrorCode(t, body) != "forbidden" {
		t.Fatalf("member: %d (%s)", forbidden.StatusCode, body)
	}

	for name, header := range map[string]string{"missing": "", "wrong": "https://attacker.test"} {
		response, body := endpointTeamUpdateDo(t, server, http.MethodPatch, target, `{"name":"Team B"}`,
			[]*http.Cookie{endpointTeamUpdateSession()}, header)
		if response.StatusCode != http.StatusForbidden || endpointTeamUpdateErrorCode(t, body) != "origin_denied" {
			t.Fatalf("%s origin: %d (%s)", name, response.StatusCode, body)
		}
	}
	if service.calls != 0 {
		t.Fatalf("guarded requests reached UpdateTeam %d times", service.calls)
	}
}

func TestEndpointTeamUpdateReturnsUpdatedTeam(t *testing.T) {
	auth, service := endpointTeamUpdateAdmin(t)
	service.team = endpointTeamUpdateTeam()
	server := newEndpointTeamUpdateServer(t, auth,
		NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	memberID := uuid.MustParse("5b8c6c5f-bf83-4a8d-8f3c-7d2e3f7b1e43")
	body := `{"name":null,"member_ids":["` + memberID.String() + `"]}`
	response, payload := endpointTeamUpdateDo(t, server, http.MethodPatch,
		"/api/admin/teams/"+service.team.ID.String(), body,
		[]*http.Cookie{endpointTeamUpdateSession()}, "https://example.test")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, payload)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode team body %q: %v", payload, err)
	}
	if decoded["id"] != service.team.ID.String() || decoded["competition_id"] != service.team.CompetitionID.String() {
		t.Fatalf("team identity %v", decoded)
	}
	if decoded["name"] != nil {
		t.Fatalf("cleared name returned as %v", decoded["name"])
	}
	if decoded["photo_url"] != "https://example.test/photo.png" || decoded["position"] != float64(2) {
		t.Fatalf("omitted scalars %v %v", decoded["photo_url"], decoded["position"])
	}
	members, ok := decoded["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members %v", decoded["members"])
	}
	if service.calls != 1 || service.id != service.team.ID {
		t.Fatalf("UpdateTeam calls=%d id=%s", service.calls, service.id)
	}
	if service.patch.Name == nil || service.patch.Name.Value != nil ||
		service.patch.PhotoURL != nil || service.patch.Position != nil ||
		service.patch.MemberIDs == nil || len(*service.patch.MemberIDs) != 1 || (*service.patch.MemberIDs)[0] != memberID {
		t.Fatalf("forwarded patch %+v", service.patch)
	}
	if !service.accountInCtx {
		t.Fatal("authenticated account missing from service context")
	}
	if !service.hasDeadline || service.deadline > 5*time.Second || service.deadline <= 0 {
		t.Fatalf("service deadline %v (present %v)", service.deadline, service.hasDeadline)
	}
}

func TestEndpointTeamUpdateRejectsInvalidRequests(t *testing.T) {
	auth, service := endpointTeamUpdateAdmin(t)
	server := newEndpointTeamUpdateServer(t, auth,
		NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	target := "/api/admin/teams/" + uuid.NewString()
	cookies := []*http.Cookie{endpointTeamUpdateSession()}
	origin := "https://example.test"
	cases := []struct {
		name   string
		target string
		body   string
		status int
		code   string
	}{
		{"invalid id", "/api/admin/teams/not-a-uuid", `{}`, http.StatusBadRequest, "invalid_id"},
		{"nil id", "/api/admin/teams/00000000-0000-0000-0000-000000000000", `{}`, http.StatusBadRequest, "invalid_id"},
		{"malformed", target, `{`, http.StatusBadRequest, "invalid_body"},
		{"unknown field", target, `{"competition_id":"` + uuid.NewString() + `"}`, http.StatusBadRequest, "invalid_body"},
		{"null members", target, `{"member_ids":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"empty members", target, `{"member_ids":[]}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"zero position", target, `{"position":0}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"duplicate members", target,
			`{"member_ids":["0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d","0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"]}`,
			http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response, body := endpointTeamUpdateDo(t, server, http.MethodPatch, testCase.target,
				testCase.body, cookies, origin)
			if response.StatusCode != testCase.status || endpointTeamUpdateErrorCode(t, body) != testCase.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
		})
	}
	if service.calls != 3 {
		t.Fatalf("validation requests reached UpdateTeam %d times, want 3", service.calls)
	}

	wrong, body := endpointTeamUpdateDo(t, server, http.MethodPost, target, `{}`, cookies, origin)
	if wrong.StatusCode != http.StatusMethodNotAllowed || endpointTeamUpdateErrorCode(t, body) != "method_not_allowed" {
		t.Fatalf("wrong method: %d (%s)", wrong.StatusCode, body)
	}
	if allow := wrong.Header.Get("Allow"); allow != "PATCH" {
		t.Fatalf("Allow %q", allow)
	}
}

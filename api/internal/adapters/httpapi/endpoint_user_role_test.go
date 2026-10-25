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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

type endpointUserRoleStore struct {
	mu       sync.Mutex
	users    map[uuid.UUID]domain.User
	sessions map[string]uuid.UUID
}

func (s *endpointUserRoleStore) current(token string) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	personID, ok := s.sessions[token]
	if !ok {
		return domain.Account{}, domain.ErrUnauthenticated
	}
	user, ok := s.users[personID]
	if !ok {
		return domain.Account{}, domain.ErrUnauthenticated
	}
	email := ""
	if user.Email != nil {
		email = *user.Email
	}
	return domain.Account{
		ID: user.ID, FirstName: user.FirstName, LastName: user.LastName, Email: email, Role: user.Role,
		CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}, nil
}

func (s *endpointUserRoleStore) update(id uuid.UUID, role string) (domain.User, error) {
	if err := application.ValidateRole(role); err != nil {
		return domain.User{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.Role = role
	s.users[id] = user
	return user, nil
}

type endpointUserRoleService struct {
	BackendService
	store *endpointUserRoleStore
	err   error

	calls       int
	id          uuid.UUID
	role        string
	hasDeadline bool
	deadline    time.Duration
}

func (s *endpointUserRoleService) UpdateUserRole(ctx context.Context, id uuid.UUID, role string) (domain.User, error) {
	s.calls++
	s.id = id
	s.role = role
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	if s.err != nil {
		return domain.User{}, s.err
	}
	return s.store.update(id, role)
}

type endpointUserRoleAuth struct {
	store *endpointUserRoleStore
}

func (*endpointUserRoleAuth) Login(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
	return domain.Account{}, "", errors.New("login unsupported")
}

func (a *endpointUserRoleAuth) CurrentUser(_ context.Context, token string) (domain.Account, error) {
	return a.store.current(token)
}

func (*endpointUserRoleAuth) Logout(context.Context, string) error {
	return errors.New("logout unsupported")
}

type endpointUserRoleFixture struct {
	service  *endpointUserRoleService
	server   *httptest.Server
	memberID uuid.UUID
	logs     *bytes.Buffer
}

func endpointUserRoleUser(id uuid.UUID, firstName, lastName, email, role string) domain.User {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return domain.User{
		ID: id, FirstName: firstName, LastName: lastName, Email: &email, Role: role,
		CreatedAt: created, UpdatedAt: created.Add(48 * time.Hour),
	}
}

func newEndpointUserRoleFixture(t *testing.T) *endpointUserRoleFixture {
	t.Helper()
	admin := endpointUserRoleUser(uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"), "Grace", "Hopper", "grace@example.com", "admin")
	member := endpointUserRoleUser(uuid.MustParse("0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"), "Ada", "Lovelace", "ada@example.com", "member")
	store := &endpointUserRoleStore{
		users:    map[uuid.UUID]domain.User{admin.ID: admin, member.ID: member},
		sessions: map[string]uuid.UUID{"admin-session": admin.ID, "member-session": member.ID},
	}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	auth := NewAuth(nil, &endpointUserRoleAuth{store: store}, AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	service := &endpointUserRoleService{store: store}
	backend := NewBackend(service, auth, logger)
	mux := http.NewServeMux()
	mux.Handle("/api/admin/users/{id}", auth.requireSameOrigin(auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodPatch: http.HandlerFunc(backend.updateUserRole),
	}))))
	mux.Handle("/api/me", auth.requireUser(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.me),
	})))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &endpointUserRoleFixture{service: service, server: server, memberID: member.ID, logs: logs}
}

func (f *endpointUserRoleFixture) do(t *testing.T, method, target, body, token string, origins []string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, f.server.URL+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for _, origin := range origins {
		request.Header.Add("Origin", origin)
	}
	if token != "" {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: token})
	}
	response, err := f.server.Client().Do(request)
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

func endpointUserRoleJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return payload
}

func endpointUserRoleCode(t *testing.T, body []byte) string {
	t.Helper()
	failure, ok := endpointUserRoleJSON(t, body)["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %s", body)
	}
	code, _ := failure["code"].(string)
	return code
}

func endpointUserRoleFields(t *testing.T, body []byte) map[string]any {
	t.Helper()
	failure, ok := endpointUserRoleJSON(t, body)["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %s", body)
	}
	fields, _ := failure["fields"].(map[string]any)
	return fields
}

func TestEndpointUserRolePromoteAndDemoteLiveSession(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	target := "/api/admin/users/" + fixture.memberID.String()
	origin := []string{"https://example.test"}
	response, body := fixture.do(t, http.MethodPatch, target, `{"role":"admin"}`, "admin-session", origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("promote status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	payload := endpointUserRoleJSON(t, body)
	publicFields := map[string]bool{
		"id": true, "first_name": true, "last_name": true,
		"email": true, "role": true, "created_at": true, "updated_at": true,
	}
	if len(payload) != len(publicFields) {
		t.Fatalf("user exposes %d fields: %v", len(payload), payload)
	}
	for key := range publicFields {
		if _, ok := payload[key]; !ok {
			t.Fatalf("user missing public field %q: %v", key, payload)
		}
	}
	if payload["id"] != fixture.memberID.String() || payload["role"] != "admin" || payload["email"] != "ada@example.com" {
		t.Fatalf("promoted payload %v", payload)
	}
	if fixture.service.calls != 1 || fixture.service.id != fixture.memberID || fixture.service.role != "admin" {
		t.Fatalf("service call %d (%s, %s)", fixture.service.calls, fixture.service.id, fixture.service.role)
	}
	if !fixture.service.hasDeadline || fixture.service.deadline <= 0 || fixture.service.deadline > 5*time.Second {
		t.Fatalf("service deadline %v (present %v)", fixture.service.deadline, fixture.service.hasDeadline)
	}

	me, meBody := fixture.do(t, http.MethodGet, "/api/me", "", "member-session", nil)
	if me.StatusCode != http.StatusOK || endpointUserRoleJSON(t, meBody)["role"] != "admin" {
		t.Fatalf("promoted existing session status %d (%s)", me.StatusCode, meBody)
	}

	response, body = fixture.do(t, http.MethodPatch, target, `{"role":"member"}`, "admin-session", origin)
	if response.StatusCode != http.StatusOK || endpointUserRoleJSON(t, body)["role"] != "member" {
		t.Fatalf("demote status %d (%s)", response.StatusCode, body)
	}
	me, meBody = fixture.do(t, http.MethodGet, "/api/me", "", "member-session", nil)
	if me.StatusCode != http.StatusOK || endpointUserRoleJSON(t, meBody)["role"] != "member" {
		t.Fatalf("demoted existing session status %d (%s)", me.StatusCode, meBody)
	}
	if fixture.service.calls != 2 || fixture.service.role != "member" {
		t.Fatalf("demote service call %d (%s)", fixture.service.calls, fixture.service.role)
	}
}

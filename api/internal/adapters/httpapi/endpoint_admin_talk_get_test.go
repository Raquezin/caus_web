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

	"caus-platform/api/internal/domain"
)

type adminTalkGetService struct {
	BackendService
	talk         domain.Talk
	err          error
	calls        int
	id           uuid.UUID
	includeUnpub bool
	deadline     time.Duration
	hasDeadline  bool
	accountInCtx bool
}

func (s *adminTalkGetService) GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	s.calls++
	s.id = id
	s.includeUnpub = includeUnpublished
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	_, s.accountInCtx = accountFromContext(ctx)
	return s.talk, s.err
}

func newEndpointAdminTalkGetServer(t *testing.T, auth *Auth, backend *Backend) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/admin/talks/{id}", auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.adminGetTalk),
	})))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointAdminTalkGetDo(t *testing.T, server *httptest.Server, method, target string, cookies []*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
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
	return response, body
}

func endpointAdminTalkGetAuth(t *testing.T, role string) *Auth {
	t.Helper()
	return newStubBackendAuth(t,
		&stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: role}}, "https://example.test")
}

func endpointAdminTalkGetSession() []*http.Cookie {
	return []*http.Cookie{{Name: "__Host-caus_session", Value: "session-token"}}
}

func endpointAdminTalkGetTalk() domain.Talk {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	stream := "https://example.test/stream"
	poster := "https://example.test/poster.png"
	return domain.Talk{
		ID:          uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"),
		Title:       "Draft talk",
		Description: "Unpublished description",
		Location:    "Madrid",
		Language:    "en",
		Date:        time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC),
		Duration:    3600,
		StreamURL:   &stream,
		PosterURL:   &poster,
		Published:   false,
		Speakers: []domain.Person{{
			ID:        uuid.MustParse("0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"),
			FirstName: "Ada",
			LastName:  "Lovelace",
		}},
		CreatedAt: created,
		UpdatedAt: created.Add(48 * time.Hour),
	}
}

func endpointAdminTalkGetJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode body %q: %v", body, err)
	}
	return payload
}

func endpointAdminTalkGetCode(t *testing.T, body []byte) string {
	t.Helper()
	failure, ok := endpointAdminTalkGetJSON(t, body)["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object: %s", body)
	}
	code, _ := failure["code"].(string)
	return code
}

func TestEndpointAdminTalkGetRequiresAdmin(t *testing.T) {
	service := &adminTalkGetService{talk: endpointAdminTalkGetTalk()}
	target := "/api/admin/talks/" + service.talk.ID.String()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	adminAuth := endpointAdminTalkGetAuth(t, "admin")
	adminServer := newEndpointAdminTalkGetServer(t, adminAuth, NewBackend(service, adminAuth, logger))
	missing, missingBody := endpointAdminTalkGetDo(t, adminServer, http.MethodGet, target, nil)
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing session status %d (%s)", missing.StatusCode, missingBody)
	}
	if code := endpointAdminTalkGetCode(t, missingBody); code != "unauthenticated" {
		t.Fatalf("missing session code %q", code)
	}

	memberAuth := endpointAdminTalkGetAuth(t, "member")
	memberServer := newEndpointAdminTalkGetServer(t, memberAuth, NewBackend(service, memberAuth, logger))
	member, memberBody := endpointAdminTalkGetDo(t, memberServer, http.MethodGet, target, endpointAdminTalkGetSession())
	if member.StatusCode != http.StatusForbidden {
		t.Fatalf("member status %d (%s)", member.StatusCode, memberBody)
	}
	if code := endpointAdminTalkGetCode(t, memberBody); code != "forbidden" {
		t.Fatalf("member code %q", code)
	}

	if service.calls != 0 {
		t.Fatalf("guarded requests reached GetTalk %d times", service.calls)
	}
	if strings.Contains(string(missingBody)+string(memberBody), "Draft talk") {
		t.Fatalf("guarded responses leaked talk data: %s %s", missingBody, memberBody)
	}
}

func TestEndpointAdminTalkGetReturnsUnpublishedTalk(t *testing.T) {
	service := &adminTalkGetService{talk: endpointAdminTalkGetTalk()}
	auth := endpointAdminTalkGetAuth(t, "admin")
	server := newEndpointAdminTalkGetServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	response, body := endpointAdminTalkGetDo(t, server, http.MethodGet, "/api/admin/talks/"+service.talk.ID.String(), endpointAdminTalkGetSession())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	payload := endpointAdminTalkGetJSON(t, body)
	if payload["id"] != service.talk.ID.String() || payload["title"] != "Draft talk" || payload["published"] != false {
		t.Fatalf("talk payload %v", payload)
	}
	if payload["date"] != "2026-11-05T18:30:00" || payload["duration"] != float64(3600) {
		t.Fatalf("talk date/duration %v %v", payload["date"], payload["duration"])
	}
	speakers, ok := payload["speakers"].([]any)
	if !ok || len(speakers) != 1 {
		t.Fatalf("speakers %v", payload["speakers"])
	}
	speaker, ok := speakers[0].(map[string]any)
	if !ok || len(speaker) != 3 {
		t.Fatalf("speaker exposes extra fields: %v", speakers[0])
	}
	if speaker["id"] != "0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d" ||
		speaker["first_name"] != "Ada" || speaker["last_name"] != "Lovelace" {
		t.Fatalf("speaker payload %v", speaker)
	}
	for _, key := range []string{"email", "google_sub", "phone", "session", "token"} {
		if _, ok := speaker[key]; ok {
			t.Fatalf("speaker exposes private field %q: %v", key, speaker)
		}
		if _, ok := payload[key]; ok {
			t.Fatalf("talk exposes private field %q: %v", key, payload)
		}
	}
}

func TestEndpointAdminTalkGetForwardsRequest(t *testing.T) {
	service := &adminTalkGetService{talk: endpointAdminTalkGetTalk()}
	auth := endpointAdminTalkGetAuth(t, "admin")
	server := newEndpointAdminTalkGetServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	response, body := endpointAdminTalkGetDo(t, server, http.MethodGet, "/api/admin/talks/"+service.talk.ID.String(), endpointAdminTalkGetSession())
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if service.calls != 1 {
		t.Fatalf("GetTalk calls %d", service.calls)
	}
	if service.id != service.talk.ID {
		t.Fatalf("forwarded id %s, want %s", service.id, service.talk.ID)
	}
	if !service.includeUnpub {
		t.Fatal("unpublished inclusion was not requested")
	}
	if !service.accountInCtx {
		t.Fatal("authenticated account missing from service context")
	}
	if !service.hasDeadline || service.deadline > 5*time.Second || service.deadline <= 0 {
		t.Fatalf("service deadline %v (present %v)", service.deadline, service.hasDeadline)
	}
}

func TestEndpointAdminTalkGetErrors(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &adminTalkGetService{}
	auth := endpointAdminTalkGetAuth(t, "admin")
	server := newEndpointAdminTalkGetServer(t, auth, NewBackend(service, auth, logger))

	for _, target := range []string{
		"/api/admin/talks/not-a-uuid",
		"/api/admin/talks/00000000-0000-0000-0000-000000000000",
	} {
		response, body := endpointAdminTalkGetDo(t, server, http.MethodGet, target, endpointAdminTalkGetSession())
		if response.StatusCode != http.StatusBadRequest || endpointAdminTalkGetCode(t, body) != "invalid_id" {
			t.Fatalf("%s status %d code %q (%s)", target, response.StatusCode, endpointAdminTalkGetCode(t, body), body)
		}
	}
	if service.calls != 0 {
		t.Fatalf("invalid ids reached GetTalk %d times", service.calls)
	}

	service.err = domain.ErrNotFound
	response, body := endpointAdminTalkGetDo(t, server, http.MethodGet, "/api/admin/talks/"+uuid.NewString(), endpointAdminTalkGetSession())
	if response.StatusCode != http.StatusNotFound || endpointAdminTalkGetCode(t, body) != "not_found" {
		t.Fatalf("missing talk status %d (%s)", response.StatusCode, body)
	}

	service.err = errors.New("pgx: password authentication failed for caus")
	response, body = endpointAdminTalkGetDo(t, server, http.MethodGet, "/api/admin/talks/"+uuid.NewString(), endpointAdminTalkGetSession())
	if response.StatusCode != http.StatusInternalServerError || endpointAdminTalkGetCode(t, body) != "internal_error" {
		t.Fatalf("failure status %d (%s)", response.StatusCode, body)
	}
	if strings.Contains(string(body), "password") || strings.Contains(logs.String(), "password") {
		t.Fatalf("failure leaked detail: body %s logs %s", body, logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
}

func TestEndpointAdminTalkGetMethodNotAllowed(t *testing.T) {
	service := &adminTalkGetService{talk: endpointAdminTalkGetTalk()}
	auth := endpointAdminTalkGetAuth(t, "admin")
	server := newEndpointAdminTalkGetServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	response, body := endpointAdminTalkGetDo(t, server, http.MethodPost, "/api/admin/talks/"+service.talk.ID.String(), endpointAdminTalkGetSession())
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if allow := response.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow %q", allow)
	}
	if code := endpointAdminTalkGetCode(t, body); code != "method_not_allowed" {
		t.Fatalf("code %q", code)
	}
	if service.calls != 0 {
		t.Fatalf("wrong method reached GetTalk %d times", service.calls)
	}
}

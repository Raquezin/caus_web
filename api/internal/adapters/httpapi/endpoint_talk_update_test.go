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

	"caus-platform/api/internal/domain"
)

type endpointTalkUpdateService struct {
	BackendService
	talk        domain.Talk
	err         error
	calls       int
	id          uuid.UUID
	patch       domain.TalkPatch
	deadline    time.Duration
	hasDeadline bool
	account     bool
}

func (s *endpointTalkUpdateService) UpdateTalk(ctx context.Context, id uuid.UUID, patch domain.TalkPatch) (domain.Talk, error) {
	s.calls++
	s.id = id
	s.patch = patch
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	_, s.account = accountFromContext(ctx)
	return s.talk, s.err
}

func endpointTalkUpdateAuth(t *testing.T, role string) *Auth {
	t.Helper()
	return newStubBackendAuth(t,
		&stubBackendAuth{account: domain.Account{ID: uuid.New(), Role: role}}, "https://example.test")
}

func newEndpointTalkUpdateServer(t *testing.T, auth *Auth, backend *Backend) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/admin/talks/{id}", auth.requireAdmin(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodPatch: http.HandlerFunc(backend.updateTalk),
	}))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointTalkUpdateTalk() domain.Talk {
	poster := "https://example.test/poster.png"
	return domain.Talk{
		ID:          uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"),
		Title:       "Updated title",
		Description: "Description",
		Location:    "Madrid",
		Language:    "en",
		Date:        time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC),
		Duration:    1800,
		PosterURL:   &poster,
		Published:   false,
		Speakers: []domain.Person{{
			ID:        uuid.MustParse("0b1c2d3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"),
			FirstName: "Ada",
			LastName:  "Lovelace",
		}},
		CreatedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 6, 5, 6, 7, 0, time.UTC),
	}
}

func endpointTalkUpdateDo(t *testing.T, server *httptest.Server, method, target, body string,
	cookies []*http.Cookie, origin string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, server.URL+target, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
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
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, payload
}

func endpointTalkUpdateCookies() []*http.Cookie {
	return []*http.Cookie{{Name: "__Host-caus_session", Value: "session-token"}}
}

func endpointTalkUpdateErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return payload.Error.Code
}

const endpointTalkUpdateBody = `{"title":"Updated title","published":false,"stream_url":null,` +
	`"poster_url":"https://example.test/poster.png","duration":1800}`

func TestEndpointTalkUpdateUpdatesTalk(t *testing.T) {
	talk := endpointTalkUpdateTalk()
	service := &endpointTalkUpdateService{talk: talk}
	auth := endpointTalkUpdateAuth(t, "admin")
	server := newEndpointTalkUpdateServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	target := "/api/admin/talks/" + talk.ID.String()

	response, body := endpointTalkUpdateDo(t, server, http.MethodPatch, target, endpointTalkUpdateBody,
		endpointTalkUpdateCookies(), "https://example.test")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode talk body %q: %v", body, err)
	}
	if payload["id"] != talk.ID.String() || payload["title"] != "Updated title" || payload["published"] != false {
		t.Fatalf("talk payload %v", payload)
	}
	if payload["date"] != "2026-11-05T18:30:00" || payload["duration"] != float64(1800) ||
		payload["stream_url"] != nil || payload["poster_url"] != "https://example.test/poster.png" {
		t.Fatalf("talk fields %v", payload)
	}
	if speakers, ok := payload["speakers"].([]any); !ok || len(speakers) != 1 {
		t.Fatalf("speakers %v", payload["speakers"])
	}
	if service.calls != 1 || service.id != talk.ID {
		t.Fatalf("UpdateTalk calls %d id %s", service.calls, service.id)
	}
	patch := service.patch
	if patch.Title == nil || *patch.Title != "Updated title" || patch.Published == nil || *patch.Published {
		t.Fatalf("forwarded patch %+v", patch)
	}
	if patch.StreamURL == nil || patch.StreamURL.Value != nil || patch.PosterURL == nil ||
		patch.PosterURL.Value == nil || patch.Duration == nil || *patch.Duration != 1800 {
		t.Fatalf("forwarded nullable fields %+v", patch)
	}
	if patch.Description != nil || patch.Date != nil || patch.Location != nil ||
		patch.Language != nil || patch.SpeakerIDs != nil {
		t.Fatalf("omitted patch fields set %+v", patch)
	}
	if !service.account {
		t.Fatal("authenticated account missing from service context")
	}
	if !service.hasDeadline || service.deadline <= 0 || service.deadline > 5*time.Second {
		t.Fatalf("service deadline %v (present %v)", service.deadline, service.hasDeadline)
	}
}

func TestEndpointTalkUpdateGuards(t *testing.T) {
	service := &endpointTalkUpdateService{talk: endpointTalkUpdateTalk()}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	target := "/api/admin/talks/" + service.talk.ID.String()

	adminAuth := endpointTalkUpdateAuth(t, "admin")
	adminServer := newEndpointTalkUpdateServer(t, adminAuth, NewBackend(service, adminAuth, logger))
	missing, missingBody := endpointTalkUpdateDo(t, adminServer, http.MethodPatch, target, endpointTalkUpdateBody,
		nil, "https://example.test")
	if missing.StatusCode != http.StatusUnauthorized || endpointTalkUpdateErrorCode(t, missingBody) != "unauthenticated" {
		t.Fatalf("missing session %d (%s)", missing.StatusCode, missingBody)
	}

	memberAuth := endpointTalkUpdateAuth(t, "member")
	memberServer := newEndpointTalkUpdateServer(t, memberAuth, NewBackend(service, memberAuth, logger))
	member, memberBody := endpointTalkUpdateDo(t, memberServer, http.MethodPatch, target, endpointTalkUpdateBody,
		endpointTalkUpdateCookies(), "https://example.test")
	if member.StatusCode != http.StatusForbidden || endpointTalkUpdateErrorCode(t, memberBody) != "forbidden" {
		t.Fatalf("member %d (%s)", member.StatusCode, memberBody)
	}

	for _, origin := range []string{"", "https://evil.test"} {
		response, body := endpointTalkUpdateDo(t, adminServer, http.MethodPatch, target, endpointTalkUpdateBody,
			endpointTalkUpdateCookies(), origin)
		if response.StatusCode != http.StatusForbidden || endpointTalkUpdateErrorCode(t, body) != "origin_denied" {
			t.Fatalf("origin %q status %d (%s)", origin, response.StatusCode, body)
		}
	}

	wrong, wrongBody := endpointTalkUpdateDo(t, adminServer, http.MethodGet, target, "",
		endpointTalkUpdateCookies(), "https://example.test")
	if wrong.StatusCode != http.StatusMethodNotAllowed || wrong.Header.Get("Allow") != "PATCH" {
		t.Fatalf("wrong method %d Allow %q (%s)", wrong.StatusCode, wrong.Header.Get("Allow"), wrongBody)
	}
	if service.calls != 0 {
		t.Fatalf("guarded requests reached UpdateTalk %d times", service.calls)
	}
}

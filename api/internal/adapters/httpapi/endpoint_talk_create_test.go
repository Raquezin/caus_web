package httpapi

import (
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

const endpointTalkCreateOrigin = "https://example.test"

type stubEndpointTalkCreateService struct {
	BackendService
	calls int
	input domain.TalkInput
	talk  domain.Talk
	err   error
}

func (s *stubEndpointTalkCreateService) CreateTalk(_ context.Context, input domain.TalkInput) (domain.Talk, error) {
	s.calls++
	s.input = input
	if s.err != nil {
		return domain.Talk{}, s.err
	}
	return s.talk, nil
}

type stubEndpointTalkCreateRepo struct {
	application.BackendRepository
	calls int
}

func (s *stubEndpointTalkCreateRepo) CreateTalk(context.Context, domain.TalkInput) (domain.Talk, error) {
	s.calls++
	return domain.Talk{}, errors.New("repository must not be called")
}

func endpointTalkCreateAccount(role string) domain.Account {
	created := time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC)
	return domain.Account{
		ID:        uuid.MustParse("0d9a4d47-8f9a-4b2f-9a1a-7a0f1c24b6d5"),
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Role:      role,
		CreatedAt: created,
		UpdatedAt: created,
	}
}

func endpointTalkCreateAuth(t *testing.T, role string) *Auth {
	t.Helper()
	authService := &stubBackendAuth{account: endpointTalkCreateAccount(role)}
	return newStubBackendAuth(t, authService, endpointTalkCreateOrigin)
}

func endpointTalkCreateLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func endpointTalkCreateServer(t *testing.T, service BackendService, auth *Auth, log *slog.Logger) *httptest.Server {
	t.Helper()
	backend := NewBackend(service, auth, log)
	mux := http.NewServeMux()
	mux.Handle("POST /api/admin/talks", auth.requireAdmin(auth.requireSameOrigin(http.HandlerFunc(backend.createTalk))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointTalkCreateDo(t *testing.T, server *httptest.Server, contentType, body, origin string, signedIn bool) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/talks", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if signedIn {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
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

func endpointTalkCreateFields(speakerIDs ...uuid.UUID) map[string]any {
	return map[string]any{
		"title":       "Concurrency",
		"description": "Description",
		"location":    "Room 1",
		"language":    "en",
		"date":        "2026-05-01T18:00:00",
		"duration":    5400,
		"speaker_ids": speakerIDs,
	}
}

func endpointTalkCreateJSON(t *testing.T, fields map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func endpointTalkCreateString(value string) *string {
	return &value
}

func endpointTalkCreateError(t *testing.T, body []byte) (string, map[string]string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return payload.Error.Code, payload.Error.Fields
}

func TestEndpointTalkCreateSuccess(t *testing.T) {
	speakerID := uuid.MustParse("3b6f0b6e-4c1a-4f7e-9a3e-1d2c3b4a5f60")
	talkID := uuid.MustParse("9c8b7a65-4321-4fed-8cba-9876543210fe")
	created := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	eventDate := time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)
	service := &stubEndpointTalkCreateService{talk: domain.Talk{
		ID:          talkID,
		Title:       "Concurrency",
		Description: "Description",
		Location:    "Room 1",
		Language:    "en",
		Date:        eventDate,
		Duration:    5400,
		StreamURL:   endpointTalkCreateString("https://example.com/stream"),
		PosterURL:   endpointTalkCreateString("https://example.com/poster.png"),
		Published:   true,
		Speakers:    []domain.Person{{ID: speakerID, FirstName: "Ada", LastName: "Lovelace"}},
		CreatedAt:   created,
		UpdatedAt:   created,
	}}
	server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())

	body := endpointTalkCreateFields(speakerID)
	body["stream_url"] = "https://example.com/stream"
	body["poster_url"] = "https://example.com/poster.png"
	body["published"] = true
	response, payload := endpointTalkCreateDo(t, server, "application/json; charset=utf-8",
		endpointTalkCreateJSON(t, body), endpointTalkCreateOrigin, true)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status %d (%s)", response.StatusCode, payload)
	}
	if location := response.Header.Get("Location"); location != "/api/admin/talks/"+talkID.String() {
		t.Fatalf("Location %q", location)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if service.calls != 1 {
		t.Fatalf("service calls %d", service.calls)
	}
	input := service.input
	if input.Title != "Concurrency" || input.Description != "Description" || input.Location != "Room 1" ||
		input.Language != "en" || input.Duration != 5400 || !input.Published {
		t.Fatalf("decoded input %+v", input)
	}
	if !input.Date.Equal(eventDate) {
		t.Fatalf("decoded date %v, want %v", input.Date, eventDate)
	}
	if input.StreamURL == nil || *input.StreamURL != "https://example.com/stream" ||
		input.PosterURL == nil || *input.PosterURL != "https://example.com/poster.png" {
		t.Fatalf("decoded urls %+v", input)
	}
	if len(input.SpeakerIDs) != 1 || input.SpeakerIDs[0] != speakerID {
		t.Fatalf("decoded speaker ids %v", input.SpeakerIDs)
	}

	var talk map[string]any
	if err := json.Unmarshal(payload, &talk); err != nil {
		t.Fatalf("decode talk body %q: %v", payload, err)
	}
	if talk["id"] != talkID.String() || talk["title"] != "Concurrency" || talk["date"] != "2026-05-01T18:00:00" ||
		talk["duration"] != float64(5400) || talk["published"] != true || talk["language"] != "en" {
		t.Fatalf("talk payload %v", talk)
	}
	if talk["stream_url"] != "https://example.com/stream" || talk["poster_url"] != "https://example.com/poster.png" {
		t.Fatalf("talk payload urls %v", talk)
	}
	speakers, ok := talk["speakers"].([]any)
	if !ok || len(speakers) != 1 || speakers[0].(map[string]any)["id"] != speakerID.String() {
		t.Fatalf("talk payload speakers %v", talk["speakers"])
	}
	if created, err := time.Parse(time.RFC3339Nano, talk["created_at"].(string)); err != nil || !created.Equal(time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at %v (%v)", talk["created_at"], err)
	}
	for _, key := range []string{"google_sub", "email", "speaker_ids"} {
		if _, ok := talk[key]; ok {
			t.Fatalf("talk payload exposes %q: %v", key, talk)
		}
	}
}

func TestEndpointTalkCreateDefaults(t *testing.T) {
	speakerID := uuid.New()
	eventDate := time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)
	service := &stubEndpointTalkCreateService{talk: domain.Talk{
		ID:        uuid.New(),
		Title:     "Concurrency",
		Date:      eventDate,
		Duration:  3600,
		Language:  "es",
		Speakers:  []domain.Person{{ID: speakerID, FirstName: "Ada", LastName: "Lovelace"}},
		CreatedAt: eventDate,
		UpdatedAt: eventDate,
	}}
	server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())

	body := map[string]any{
		"title":       "Concurrency",
		"description": "Description",
		"location":    "Room 1",
		"date":        "2026-05-01T18:00:00",
		"duration":    3600,
		"speaker_ids": []uuid.UUID{speakerID},
	}
	response, payload := endpointTalkCreateDo(t, server, "application/json",
		endpointTalkCreateJSON(t, body), endpointTalkCreateOrigin, true)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status %d (%s)", response.StatusCode, payload)
	}
	if service.input.Language != "es" || service.input.Published || service.input.StreamURL != nil || service.input.PosterURL != nil {
		t.Fatalf("defaults %+v", service.input)
	}
	var talk map[string]any
	if err := json.Unmarshal(payload, &talk); err != nil {
		t.Fatal(err)
	}
	if talk["language"] != "es" || talk["published"] != false {
		t.Fatalf("payload defaults %v", talk)
	}
	for _, key := range []string{"stream_url", "poster_url"} {
		if value, ok := talk[key]; !ok || value != nil {
			t.Fatalf("payload %q = %v (present %v)", key, value, ok)
		}
	}
	if speakers, ok := talk["speakers"].([]any); !ok || len(speakers) != 1 {
		t.Fatalf("payload speakers %v", talk["speakers"])
	}
}

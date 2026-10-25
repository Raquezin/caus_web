package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type talkGetBackendService struct {
	BackendService
	talk                  domain.Talk
	err                   error
	gotID                 uuid.UUID
	gotIncludeUnpublished bool
	gotCtx                context.Context
	calls                 int
}

func (s *talkGetBackendService) GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	s.calls++
	s.gotID = id
	s.gotIncludeUnpublished = includeUnpublished
	s.gotCtx = ctx
	return s.talk, s.err
}

func newTalkGetHandler(service BackendService, logs *bytes.Buffer) http.Handler {
	var logger *slog.Logger
	if logs != nil {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	} else {
		logger = slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	}
	backend := NewBackend(service, nil, logger)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/talks/{id}", backend.getTalk)
	return mux
}

func serveTalkGet(handler http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestEndpointTalkGet(t *testing.T) {
	stream := "https://example.com/stream"
	poster := "https://example.com/poster"
	speaker := domain.Person{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}
	talk := domain.Talk{
		ID:          uuid.New(),
		Title:       "Concurrency",
		Description: "Goroutines",
		Location:    "Room 1",
		Language:    "en",
		Date:        time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC),
		Duration:    5400,
		StreamURL:   &stream,
		PosterURL:   &poster,
		Published:   true,
		Speakers:    []domain.Person{speaker},
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}
	service := &talkGetBackendService{talk: talk}
	recorder := serveTalkGet(newTalkGetHandler(service, nil), "/api/talks/"+talk.ID.String())
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		ID          string           `json:"id"`
		Title       string           `json:"title"`
		Description string           `json:"description"`
		Date        string           `json:"date"`
		Duration    int64            `json:"duration"`
		Location    string           `json:"location"`
		Language    string           `json:"language"`
		StreamURL   *string          `json:"stream_url"`
		PosterURL   *string          `json:"poster_url"`
		Published   bool             `json:"published"`
		Speakers    []map[string]any `json:"speakers"`
		CreatedAt   string           `json:"created_at"`
		UpdatedAt   string           `json:"updated_at"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	if body.ID != talk.ID.String() || body.Title != "Concurrency" || body.Description != "Goroutines" ||
		body.Date != "2026-03-14T09:30:00" || body.Duration != 5400 || body.Location != "Room 1" ||
		body.Language != "en" || body.StreamURL == nil || body.PosterURL == nil || !body.Published ||
		body.CreatedAt != "2026-01-02T03:04:05Z" || body.UpdatedAt != "2026-01-02T03:04:06Z" {
		t.Fatalf("unexpected body %s", recorder.Body.String())
	}
	if len(body.Speakers) != 1 {
		t.Fatalf("speakers %+v", body.Speakers)
	}
	if len(body.Speakers[0]) != 3 || body.Speakers[0]["id"] != speaker.ID.String() ||
		body.Speakers[0]["first_name"] != "Ada" || body.Speakers[0]["last_name"] != "Lovelace" {
		t.Fatalf("speaker keys %+v", body.Speakers[0])
	}
	for _, leaked := range []string{"email", "google_sub", "private"} {
		if strings.Contains(recorder.Body.String(), leaked) {
			t.Fatalf("body leaks %q: %s", leaked, recorder.Body.String())
		}
	}
	if service.calls != 1 || service.gotID != talk.ID || service.gotIncludeUnpublished {
		t.Fatalf("service call id=%v includeUnpublished=%v calls=%d",
			service.gotID, service.gotIncludeUnpublished, service.calls)
	}
	if service.gotCtx == nil {
		t.Fatal("service received no context")
	}
	if _, ok := service.gotCtx.Deadline(); !ok {
		t.Fatal("service context has no deadline")
	}

	empty := &talkGetBackendService{talk: domain.Talk{ID: talk.ID, Speakers: nil}}
	recorder = serveTalkGet(newTalkGetHandler(empty, nil), "/api/talks/"+talk.ID.String())
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"speakers":[]`) {
		t.Fatalf("empty speakers body %s", recorder.Body.String())
	}
}

func TestEndpointTalkGetNotFound(t *testing.T) {
	id := uuid.New()
	for _, err := range []error{domain.ErrNotFound, fmt.Errorf("lookup: %w", domain.ErrNotFound)} {
		service := &talkGetBackendService{err: err}
		recorder := serveTalkGet(newTalkGetHandler(service, nil), "/api/talks/"+id.String())
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "not_found" {
			t.Fatalf("code %q", code)
		}
		if service.calls != 1 || service.gotIncludeUnpublished {
			t.Fatalf("public lookup includeUnpublished=%v calls=%d", service.gotIncludeUnpublished, service.calls)
		}
	}
}

func TestEndpointTalkGetInvalidID(t *testing.T) {
	for _, target := range []string{"/api/talks/not-a-uuid", "/api/talks/" + uuid.Nil.String()} {
		service := &talkGetBackendService{}
		recorder := serveTalkGet(newTalkGetHandler(service, nil), target)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d: %s", target, recorder.Code, recorder.Body.String())
		}
		if code := errorCode(t, recorder); code != "invalid_id" {
			t.Fatalf("%s: code %q", target, code)
		}
		if service.calls != 0 {
			t.Fatalf("%s: service called %d times", target, service.calls)
		}
	}
}

func TestEndpointTalkGetServiceFailure(t *testing.T) {
	var logs bytes.Buffer
	service := &talkGetBackendService{err: errors.New("sensitive database detail")}
	recorder := serveTalkGet(newTalkGetHandler(service, &logs), "/api/talks/"+uuid.NewString())
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if code := errorCode(t, recorder); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "sensitive") || strings.Contains(body, "database detail") {
		t.Fatalf("body leaked error: %s", body)
	}
	if strings.Contains(logs.String(), "sensitive database detail") {
		t.Fatalf("logs leaked error: %s", logs.String())
	}
}

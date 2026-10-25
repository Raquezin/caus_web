package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

type endpointTalksListContextKey struct{}

type endpointTalksListContextService struct {
	BackendService
	marker    any
	deadline  bool
	remaining time.Duration
	err       error
}

func (s *endpointTalksListContextService) ListTalks(
	ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	s.marker = ctx.Value(endpointTalksListContextKey{})
	deadline, ok := ctx.Deadline()
	s.deadline = ok
	if ok {
		s.remaining = time.Until(deadline)
	}
	s.err = ctx.Err()
	return domain.TalkPage{}, nil
}

func endpointTalksListDirect(service BackendService, ctx context.Context, target string) *httptest.ResponseRecorder {
	backend := NewBackend(service, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	backend.listTalks(recorder, httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx))
	return recorder
}

func TestEndpointTalksListDirectHandlerKeepsRequestContext(t *testing.T) {
	service := &endpointTalksListContextService{}
	ctx := context.WithValue(context.Background(), endpointTalksListContextKey{}, "request-marker")
	recorder := endpointTalksListDirect(service, ctx, "/api/talks?limit=3&offset=2")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body)
	}
	if service.marker != "request-marker" {
		t.Fatalf("request context value %v", service.marker)
	}
	if !service.deadline || service.remaining <= 0 || service.remaining > 5*time.Second {
		t.Fatalf("deadline present %v remaining %v, want a deadline within 5s", service.deadline, service.remaining)
	}
	if service.err != nil {
		t.Fatalf("request context error %v", service.err)
	}
}

func TestEndpointTalksListDirectHandlerInheritsCancellation(t *testing.T) {
	service := &endpointTalksListContextService{}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := endpointTalksListDirect(service, parent, "/api/talks")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body)
	}
	if service.err != context.Canceled {
		t.Fatalf("request context error %v, want context.Canceled", service.err)
	}
	if !service.deadline {
		t.Fatal("cancelled request lost the lookup deadline")
	}
}

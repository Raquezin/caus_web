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

type fakeEndpointAdminTalksListBackend struct {
	BackendService
	calls     int
	page      domain.Page
	filter    domain.TalkFilter
	ctx       context.Context
	remaining time.Duration
	result    domain.TalkPage
	err       error
}

func (f *fakeEndpointAdminTalksListBackend) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	f.calls++
	f.page = page
	f.filter = filter
	f.ctx = ctx
	if deadline, ok := ctx.Deadline(); ok {
		f.remaining = time.Until(deadline)
	}
	return f.result, f.err
}

func endpointAdminTalksListHandler(t *testing.T, service BackendService, role string, logs io.Writer) http.Handler {
	t.Helper()
	if logs == nil {
		logs = io.Discard
	}
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Role: role},
	}, "https://example.test")
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(logs, nil)))
	return auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.adminListTalks),
	}))
}

func endpointAdminTalksListGet(t *testing.T, handler http.Handler, target string, authenticated bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if authenticated {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return recorder, payload
}

func TestEndpointAdminTalksListRejectsUnauthenticated(t *testing.T) {
	service := &fakeEndpointAdminTalksListBackend{}
	handler := endpointAdminTalksListHandler(t, service, "admin", nil)

	recorder, _ := endpointAdminTalksListGet(t, handler, "/api/admin/talks", false)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "unauthenticated" {
		t.Fatalf("code %q, want unauthenticated", code)
	}
	if service.calls != 0 {
		t.Fatalf("service called %d times without a session", service.calls)
	}
}

func TestEndpointAdminTalksListRejectsMember(t *testing.T) {
	service := &fakeEndpointAdminTalksListBackend{}
	handler := endpointAdminTalksListHandler(t, service, "member", nil)

	recorder, _ := endpointAdminTalksListGet(t, handler, "/api/admin/talks", true)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
	if code := errorCode(t, recorder); code != "forbidden" {
		t.Fatalf("code %q, want forbidden", code)
	}
	if service.calls != 0 {
		t.Fatalf("service called %d times for a member", service.calls)
	}
}

func TestEndpointAdminTalksListReturnsUnpublishedForAdmin(t *testing.T) {
	published := domain.Talk{ID: uuid.New(), Title: "Live", Published: true}
	draft := domain.Talk{ID: uuid.New(), Title: "Draft", Published: false}
	service := &fakeEndpointAdminTalksListBackend{result: domain.TalkPage{
		Talks:   []domain.Talk{published, draft},
		HasMore: true,
	}}
	handler := endpointAdminTalksListHandler(t, service, "admin", nil)

	recorder, payload := endpointAdminTalksListGet(t, handler, "/api/admin/talks?limit=7&offset=3&period=upcoming", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q, want application/json", ct)
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data %v, want two talks", payload["data"])
	}
	first, _ := data[0].(map[string]any)
	second, _ := data[1].(map[string]any)
	if first["id"] != published.ID.String() || first["published"] != true {
		t.Fatalf("published talk %v", first)
	}
	if second["id"] != draft.ID.String() || second["published"] != false {
		t.Fatalf("draft talk %v", second)
	}
	pagination, _ := payload["pagination"].(map[string]any)
	if pagination["limit"] != float64(7) || pagination["offset"] != float64(3) || pagination["has_more"] != true {
		t.Fatalf("pagination %v", pagination)
	}
	if service.calls != 1 {
		t.Fatalf("service calls %d, want 1", service.calls)
	}
	if service.page != (domain.Page{Limit: 7, Offset: 3}) {
		t.Fatalf("page %+v not passed through", service.page)
	}
	if !service.filter.IncludeUnpublished {
		t.Fatalf("filter %+v, want unpublished included", service.filter)
	}
	if service.filter.Period != domain.TalkPeriodUpcoming {
		t.Fatalf("period %q, want upcoming", service.filter.Period)
	}
	if service.filter.Now.Location() != time.UTC {
		t.Fatalf("filter now %v is not a UTC wall clock", service.filter.Now)
	}
	if service.remaining <= 0 || service.remaining > 5*time.Second {
		t.Fatalf("remaining deadline %v, want positive and at most 5s", service.remaining)
	}
}

type endpointAdminTalksListContextKey struct{}

func TestEndpointAdminTalksListPropagatesRequestContext(t *testing.T) {
	service := &fakeEndpointAdminTalksListBackend{result: domain.TalkPage{}}
	handler := endpointAdminTalksListHandler(t, service, "admin", nil)

	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), endpointAdminTalksListContextKey{}, "trace-value"), 2*time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/talks", nil).WithContext(parent)
	request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if service.calls != 1 {
		t.Fatalf("service calls %d, want 1", service.calls)
	}
	if got := service.ctx.Value(endpointAdminTalksListContextKey{}); got != "trace-value" {
		t.Fatalf("context value %v, want trace-value", got)
	}
	if service.remaining <= 0 || service.remaining > 2*time.Second {
		t.Fatalf("remaining deadline %v, want positive and within the shorter parent deadline", service.remaining)
	}
	if err := service.ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("derived context error %v, want canceled after the handler returned", err)
	}
}

func TestEndpointAdminTalksListEmptyResultIsJSONArray(t *testing.T) {
	service := &fakeEndpointAdminTalksListBackend{result: domain.TalkPage{}}
	handler := endpointAdminTalksListHandler(t, service, "admin", nil)

	recorder, payload := endpointAdminTalksListGet(t, handler, "/api/admin/talks", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"data":[]`) {
		t.Fatalf("body %q, want an empty data array", recorder.Body.String())
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 0 {
		t.Fatalf("data %v, want empty array", payload["data"])
	}
	pagination, _ := payload["pagination"].(map[string]any)
	if pagination["limit"] != float64(domain.DefaultPageLimit) || pagination["offset"] != float64(0) || pagination["has_more"] != false {
		t.Fatalf("pagination %v", pagination)
	}
	if service.page != (domain.Page{Limit: domain.DefaultPageLimit}) {
		t.Fatalf("page %+v, want defaults", service.page)
	}
	if service.filter.Period != domain.TalkPeriodAll || !service.filter.IncludeUnpublished {
		t.Fatalf("filter %+v, want all including unpublished", service.filter)
	}
}

func TestEndpointAdminTalksListMalformedQueryIs400(t *testing.T) {
	for _, target := range []string{
		"/api/admin/talks?limit=0",
		"/api/admin/talks?limit=101",
		"/api/admin/talks?limit=two",
		"/api/admin/talks?limit=1&limit=2",
		"/api/admin/talks?offset=-1",
		"/api/admin/talks?offset=x",
		"/api/admin/talks?period=future",
		"/api/admin/talks?period=",
		"/api/admin/talks?period=all&period=past",
		"/api/admin/talks?period=%zz",
	} {
		service := &fakeEndpointAdminTalksListBackend{}
		handler := endpointAdminTalksListHandler(t, service, "admin", nil)

		recorder, payload := endpointAdminTalksListGet(t, handler, target, true)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", target, recorder.Code)
		}
		errorPayload, ok := payload["error"].(map[string]any)
		if !ok {
			t.Fatalf("%s: error %v", target, payload["error"])
		}
		code, _ := errorPayload["code"].(string)
		message, _ := errorPayload["message"].(string)
		if code == "" || message == "" {
			t.Fatalf("%s: error %v", target, payload["error"])
		}
		if service.calls != 0 {
			t.Fatalf("%s: service called %d times for malformed query", target, service.calls)
		}
	}
}

func TestEndpointAdminTalksListBackendFailureIsSanitized(t *testing.T) {
	var logs bytes.Buffer
	service := &fakeEndpointAdminTalksListBackend{err: errors.New("sensitive database detail")}
	handler := endpointAdminTalksListHandler(t, service, "admin", &logs)

	recorder, payload := endpointAdminTalksListGet(t, handler, "/api/admin/talks?limit=2", true)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", recorder.Code)
	}
	errorPayload, _ := payload["error"].(map[string]any)
	if errorPayload["code"] != "internal_error" {
		t.Fatalf("error %v, want internal_error", payload["error"])
	}
	if strings.Contains(recorder.Body.String(), "sensitive") {
		t.Fatalf("body leaked backend error: %s", recorder.Body.String())
	}
	if strings.Contains(logs.String(), "sensitive") {
		t.Fatalf("logs leaked backend error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
}

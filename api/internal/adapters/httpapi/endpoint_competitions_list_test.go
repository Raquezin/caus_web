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
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointCompetitionsListService struct {
	BackendService
	gotCtx  context.Context
	gotPage domain.Page
	result  domain.CompetitionPage
	err     error
	calls   int
}

func (s *endpointCompetitionsListService) ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error) {
	s.calls++
	s.gotCtx = ctx
	s.gotPage = page
	return s.result, s.err
}

func newEndpointCompetitionsListHandler(service BackendService, logs io.Writer) http.Handler {
	backend := NewBackend(service, nil, slog.New(slog.NewJSONHandler(logs, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/competitions", backend.listCompetitions)
	return mux
}

func endpointCompetitionsListKeys(t *testing.T, value map[string]any) []string {
	t.Helper()
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func endpointCompetitionsListBody(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Data       []map[string]any `json:"data"`
	Pagination struct {
		Limit   int  `json:"limit"`
		Offset  int  `json:"offset"`
		HasMore bool `json:"has_more"`
	} `json:"pagination"`
} {
	t.Helper()
	var payload struct {
		Data       []map[string]any `json:"data"`
		Pagination struct {
			Limit   int  `json:"limit"`
			Offset  int  `json:"offset"`
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestEndpointCompetitionsListReturnsPage(t *testing.T) {
	firstDate := time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first := domain.Competition{
		ID: uuid.New(), Name: "Spring", Description: "Description", Location: "Hall",
		Date: firstDate, TeamSize: 3, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	second := domain.Competition{
		ID: uuid.New(), Name: "Autumn", Description: "Description", Location: "Hall",
		Date: time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC), TeamSize: 3, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	service := &endpointCompetitionsListService{
		result: domain.CompetitionPage{Competitions: []domain.Competition{first, second}, HasMore: true},
	}
	handler := newEndpointCompetitionsListHandler(service, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/competitions?limit=2&offset=4", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content-type %q", contentType)
	}
	if service.calls != 1 || service.gotPage != (domain.Page{Limit: 2, Offset: 4}) {
		t.Fatalf("service calls %d page %+v", service.calls, service.gotPage)
	}
	payload := endpointCompetitionsListBody(t, recorder)
	if payload.Pagination.Limit != 2 || payload.Pagination.Offset != 4 || !payload.Pagination.HasMore {
		t.Fatalf("pagination %+v", payload.Pagination)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("data %v", payload.Data)
	}
	for _, item := range payload.Data {
		want := []string{"created_at", "date", "description", "id", "location", "name", "team_size", "updated_at"}
		if keys := endpointCompetitionsListKeys(t, item); strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Fatalf("competition keys %v, want %v", keys, want)
		}
	}
	if payload.Data[0]["name"] != "Spring" || payload.Data[0]["date"] != "2026-05-02T18:00:00" {
		t.Fatalf("first competition %v", payload.Data[0])
	}
	if payload.Data[0]["team_size"] != float64(3) {
		t.Fatalf("team_size %v", payload.Data[0]["team_size"])
	}
	if payload.Data[0]["created_at"] != "2026-01-02T03:04:05Z" {
		t.Fatalf("created_at %v", payload.Data[0]["created_at"])
	}
	if body := recorder.Body.String(); strings.Contains(body, "published") || strings.Contains(body, "google_sub") {
		t.Fatalf("private fields leaked: %s", body)
	}
}

func TestEndpointCompetitionsListEmptyDataIsArray(t *testing.T) {
	service := &endpointCompetitionsListService{}
	handler := newEndpointCompetitionsListHandler(service, io.Discard)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/competitions", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"data":[]`) {
		t.Fatalf("body %s, want empty data array", recorder.Body.String())
	}
	payload := endpointCompetitionsListBody(t, recorder)
	if len(payload.Data) != 0 || payload.Pagination.HasMore {
		t.Fatalf("empty page payload %+v", payload)
	}
	if service.gotPage != (domain.Page{Limit: domain.DefaultPageLimit, Offset: 0}) {
		t.Fatalf("default page %+v, want limit %d offset 0", service.gotPage, domain.DefaultPageLimit)
	}
	if payload.Pagination.Limit != 20 || payload.Pagination.Offset != 0 {
		t.Fatalf("default pagination %+v, want limit 20 offset 0", payload.Pagination)
	}

	service = &endpointCompetitionsListService{}
	handler = newEndpointCompetitionsListHandler(service, io.Discard)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/competitions?limit=100", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("limit 100 status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if service.gotPage != (domain.Page{Limit: domain.MaxPageLimit, Offset: 0}) {
		t.Fatalf("limit 100 page %+v", service.gotPage)
	}
	if limit := endpointCompetitionsListBody(t, recorder).Pagination.Limit; limit != domain.MaxPageLimit {
		t.Fatalf("limit 100 pagination limit %d", limit)
	}
}

func TestEndpointCompetitionsListRejectsInvalidPagination(t *testing.T) {
	for _, query := range []string{
		"?limit=0",
		"?limit=-1",
		"?limit=101",
		"?limit=abc",
		"?limit=1&limit=2",
		"?offset=-1",
		"?offset=abc",
		"?offset=1&offset=2",
	} {
		service := &endpointCompetitionsListService{}
		handler := newEndpointCompetitionsListHandler(service, io.Discard)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/competitions"+query, nil))

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", query, recorder.Code)
		}
		var response struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("%s: decode body %q: %v", query, recorder.Body.String(), err)
		}
		if response.Error.Code != "invalid_pagination" {
			t.Fatalf("%s: code %q", query, response.Error.Code)
		}
		if service.calls != 0 {
			t.Fatalf("%s: service called %d times", query, service.calls)
		}
	}
}

func TestEndpointCompetitionsListForwardsContext(t *testing.T) {
	service := &endpointCompetitionsListService{}
	handler := newEndpointCompetitionsListHandler(service, io.Discard)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	request := httptest.NewRequest(http.MethodGet, "/api/competitions", nil).WithContext(ctx)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if service.calls != 1 {
		t.Fatalf("service calls %d", service.calls)
	}
	if service.gotCtx.Value(contextKey{}) != "marker" {
		t.Fatal("request context was not forwarded")
	}
	deadline, ok := service.gotCtx.Deadline()
	if !ok {
		t.Fatal("service context has no deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > lookupTimeout {
		t.Fatalf("deadline remaining %v", remaining)
	}
}

func TestEndpointCompetitionsListFailureIsSanitized(t *testing.T) {
	var logs bytes.Buffer
	service := &endpointCompetitionsListService{err: errors.New("SECRET-MARKER database detail")}
	handler := newEndpointCompetitionsListHandler(service, &logs)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/competitions?offset=2", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", recorder.Code)
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "internal_error" {
		t.Fatalf("code %q", response.Error.Code)
	}
	if strings.Contains(recorder.Body.String(), "SECRET-MARKER") || strings.Contains(logs.String(), "SECRET-MARKER") {
		t.Fatalf("raw error leaked: body %s logs %s", recorder.Body.String(), logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) || !strings.Contains(logs.String(), `"operation"`) {
		t.Fatalf("missing log fields: %s", logs.String())
	}
}

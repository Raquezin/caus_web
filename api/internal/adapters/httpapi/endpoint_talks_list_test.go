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

type endpointTalksListService struct {
	BackendService
	calls  int
	page   domain.Page
	filter domain.TalkFilter
	result domain.TalkPage
	err    error
}

func (s *endpointTalksListService) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	s.calls++
	s.page = page
	s.filter = filter
	return s.result, s.err
}

func newEndpointTalksListServer(t *testing.T, service BackendService) *httptest.Server {
	t.Helper()
	backend := NewBackend(service, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	server := httptest.NewServer(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.listTalks),
	}))
	t.Cleanup(server.Close)
	return server
}

func endpointTalksListGet(t *testing.T, server *httptest.Server, target string) (*http.Response, []byte) {
	t.Helper()
	response, err := server.Client().Get(server.URL + target)
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

type endpointTalksListPayload struct {
	Data []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Date      string `json:"date"`
		Duration  int64  `json:"duration"`
		Published bool   `json:"published"`
		Speakers  []struct {
			ID        string `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"speakers"`
	} `json:"data"`
	Pagination struct {
		Limit   int  `json:"limit"`
		Offset  int  `json:"offset"`
		HasMore bool `json:"has_more"`
	} `json:"pagination"`
}

func endpointTalksListDecode(t *testing.T, body []byte) endpointTalksListPayload {
	t.Helper()
	var payload endpointTalksListPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode talks body %q: %v", body, err)
	}
	return payload
}

func endpointTalksListTalk(title string, date time.Time, speaker domain.Person) domain.Talk {
	return domain.Talk{
		ID:          uuid.New(),
		Title:       title,
		Description: "Description",
		Location:    "Room 1",
		Language:    "es",
		Date:        date,
		Duration:    1800,
		Published:   true,
		Speakers:    []domain.Person{speaker},
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}
}

func TestEndpointTalksListReturnsPublicPage(t *testing.T) {
	speaker := domain.Person{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}
	talks := []domain.Talk{
		endpointTalksListTalk("First", time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC), speaker),
		endpointTalksListTalk("Second", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), speaker),
	}
	service := &endpointTalksListService{result: domain.TalkPage{Talks: talks, HasMore: true}}
	server := newEndpointTalksListServer(t, service)

	response, body := endpointTalksListGet(t, server, "/api/talks")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if service.calls != 1 {
		t.Fatalf("service called %d times", service.calls)
	}
	if service.page != (domain.Page{Limit: domain.DefaultPageLimit}) {
		t.Fatalf("page %+v", service.page)
	}
	if service.filter.Period != domain.TalkPeriodAll || service.filter.IncludeUnpublished {
		t.Fatalf("filter %+v", service.filter)
	}
	if service.filter.Now.Location() != time.UTC ||
		service.filter.Now.Sub(naiveMadridNow(time.Now())) > 2*time.Second ||
		naiveMadridNow(time.Now()).Sub(service.filter.Now) > 2*time.Second {
		t.Fatalf("filter now %v", service.filter.Now)
	}
	payload := endpointTalksListDecode(t, body)
	if len(payload.Data) != 2 || payload.Data[0].Title != "First" || payload.Data[1].Title != "Second" {
		t.Fatalf("data %+v", payload.Data)
	}
	if payload.Data[0].Date != "2026-04-01T10:00:00" || payload.Data[0].Duration != 1800 || !payload.Data[0].Published {
		t.Fatalf("talk %+v", payload.Data[0])
	}
	if len(payload.Data[0].Speakers) != 1 || payload.Data[0].Speakers[0].LastName != "Lovelace" {
		t.Fatalf("speakers %+v", payload.Data[0].Speakers)
	}
	if payload.Pagination.Limit != domain.DefaultPageLimit || payload.Pagination.Offset != 0 || !payload.Pagination.HasMore {
		t.Fatalf("pagination %+v", payload.Pagination)
	}
	for _, leaked := range []string{"email", "google_sub", "speaker_ids"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("response leaked %q: %s", leaked, body)
		}
	}
}

func TestEndpointTalksListQueryParams(t *testing.T) {
	service := &endpointTalksListService{}
	server := newEndpointTalksListServer(t, service)
	response, body := endpointTalksListGet(t, server, "/api/talks?period=past&limit=5&offset=10")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if service.page != (domain.Page{Limit: 5, Offset: 10}) || service.filter.Period != domain.TalkPeriodPast {
		t.Fatalf("page %+v filter %+v", service.page, service.filter)
	}
	if service.filter.IncludeUnpublished {
		t.Fatal("public list requested unpublished talks")
	}
}

func TestEndpointTalksListRejectsInvalidQueries(t *testing.T) {
	cases := []struct {
		target string
		code   string
	}{
		{"/api/talks?period=week", "invalid_filter"},
		{"/api/talks?period=", "invalid_filter"},
		{"/api/talks?period=upcoming&period=past", "invalid_filter"},
		{"/api/talks?limit=0", "invalid_pagination"},
		{"/api/talks?limit=101", "invalid_pagination"},
		{"/api/talks?limit=abc", "invalid_pagination"},
		{"/api/talks?limit=1&limit=2", "invalid_pagination"},
		{"/api/talks?offset=1&offset=2", "invalid_pagination"},
		{"/api/talks?offset=-1", "invalid_pagination"},
		{"/api/talks?offset=x", "invalid_pagination"},
		{"/api/talks?limit=", "invalid_pagination"},
		{"/api/talks?offset=", "invalid_pagination"},
		{"/api/talks?limit=%zz", "invalid_pagination"},
		{"/api/talks?offset=%zz", "invalid_pagination"},
		{"/api/talks?period=%zz", "invalid_pagination"},
	}
	service := &endpointTalksListService{}
	server := newEndpointTalksListServer(t, service)
	for _, testCase := range cases {
		response, body := endpointTalksListGet(t, server, testCase.target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status %d (%s)", testCase.target, response.StatusCode, body)
		}
		var payload struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Error.Code != testCase.code {
			t.Fatalf("%s body %q (%v)", testCase.target, body, err)
		}
	}
	if service.calls != 0 {
		t.Fatalf("invalid queries reached the service %d times", service.calls)
	}
}

func TestEndpointTalksListEmptyIsArray(t *testing.T) {
	server := newEndpointTalksListServer(t, &endpointTalksListService{})
	response, body := endpointTalksListGet(t, server, "/api/talks?period=upcoming")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if !strings.Contains(string(body), `"data":[]`) {
		t.Fatalf("empty list body %s", body)
	}
	payload := endpointTalksListDecode(t, body)
	if payload.Pagination.HasMore {
		t.Fatalf("pagination %+v", payload.Pagination)
	}
}

func TestEndpointTalksListSanitizesFailures(t *testing.T) {
	service := &endpointTalksListService{err: errors.New("connection string password rejected")}
	server := newEndpointTalksListServer(t, service)
	response, body := endpointTalksListGet(t, server, "/api/talks")
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("password")) {
		t.Fatalf("failure leaked detail: %s", body)
	}
}

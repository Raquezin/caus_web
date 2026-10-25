package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestListPeopleDefaults(t *testing.T) {
	id := uuid.New()
	people := &fakePeople{page: domain.PeoplePage{
		People:  []domain.Person{{ID: id, FirstName: "Ada", LastName: "Lovelace"}},
		HasMore: true,
	}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID        string `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"data"`
		Pagination struct {
			Limit   int  `json:"limit"`
			Offset  int  `json:"offset"`
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if people.gotPage.Limit != domain.DefaultPageLimit || people.gotPage.Offset != 0 {
		t.Fatalf("page %+v, want limit %d offset 0", people.gotPage, domain.DefaultPageLimit)
	}
	if len(body.Data) != 1 || body.Data[0].ID != id.String() || body.Data[0].FirstName != "Ada" || body.Data[0].LastName != "Lovelace" {
		t.Fatalf("data %+v", body.Data)
	}
	if body.Pagination.Limit != 20 || body.Pagination.Offset != 0 || !body.Pagination.HasMore {
		t.Fatalf("pagination %+v", body.Pagination)
	}
	var raw map[string]any
	resp2 := doAdmin(t, srv, http.MethodGet, "/api/people")
	if err := json.NewDecoder(resp2.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	item := raw["data"].([]any)[0].(map[string]any)
	if len(item) != 3 {
		t.Fatalf("expected exactly 3 fields, got %v", item)
	}
	for _, private := range []string{"email", "google_sub", "role", "created_at", "updated_at"} {
		if _, ok := item[private]; ok {
			t.Fatalf("private field %q exposed", private)
		}
	}
}

func TestListPeopleCustomAndMaxPage(t *testing.T) {
	for _, tc := range []struct {
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"?limit=1&offset=0", 1, 0},
		{"?limit=100&offset=250", 100, 250},
		{"?offset=7", 20, 7},
	} {
		people := &fakePeople{page: domain.PeoplePage{People: []domain.Person{}}}
		srv, _ := newAdminTestServer(t, people, nil)
		resp := doAdmin(t, srv, http.MethodGet, "/api/people"+tc.query)
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", tc.query, resp.StatusCode)
		}
		if people.gotPage.Limit != tc.wantLimit || people.gotPage.Offset != tc.wantOffset {
			t.Fatalf("%s: page %+v", tc.query, people.gotPage)
		}
	}
}

func TestListPeopleEmptyPageReturnsEmptyArray(t *testing.T) {
	people := &fakePeople{page: domain.PeoplePage{}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people?offset=9999")
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok || data == nil {
		t.Fatalf("data %v, want []", body["data"])
	}
	if len(data) != 0 {
		t.Fatalf("data %v, want empty", data)
	}
	pagination := body["pagination"].(map[string]any)
	if pagination["has_more"] != false {
		t.Fatalf("has_more %v", pagination["has_more"])
	}
}

func TestListPeopleInvalidPagination(t *testing.T) {
	for _, query := range []string{
		"?limit=", "?limit=abc", "?limit=-1", "?limit=0", "?limit=101",
		"?limit=99999999999999999999", "?limit=1&limit=2",
		"?offset=", "?offset=abc", "?offset=-1", "?offset=99999999999999999999",
		"?offset=1&offset=2",
		"?limit=%ZZ", "?offset=%ZZ", "?limit=1;offset=2",
	} {
		people := &fakePeople{}
		srv, _ := newAdminTestServer(t, people, nil)
		resp := doAdmin(t, srv, http.MethodGet, "/api/people"+query)
		code, _ := errorBody(t, resp)
		srv.Close()
		if resp.StatusCode != http.StatusBadRequest || code != "invalid_pagination" {
			t.Fatalf("%s: status %d code %q", query, resp.StatusCode, code)
		}
		if people.called {
			t.Fatalf("%s: service called", query)
		}
	}
}

func TestListPeopleServiceInvalidPagination(t *testing.T) {
	people := &fakePeople{err: fmt.Errorf("list: %w", domain.ErrInvalidPagination)}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people")
	code, _ := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || code != "invalid_pagination" {
		t.Fatalf("status %d code %q", resp.StatusCode, code)
	}
}

func TestListPeopleInternalErrorSanitized(t *testing.T) {
	people := &fakePeople{err: errors.New("password=SECRET-MARKER db exploded")}
	srv, logBuf := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "SECRET-MARKER") {
		t.Fatalf("secret leaked in response: %s", body)
	}
	if strings.Contains(logBuf.String(), "SECRET-MARKER") {
		t.Fatalf("secret leaked in logs: %s", logBuf.String())
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error.Code != "internal_error" {
		t.Fatalf("code %q", parsed.Error.Code)
	}
}

func TestListPeopleContextBounded(t *testing.T) {
	people := &fakePeople{page: domain.PeoplePage{People: []domain.Person{}}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()
	resp := doAdmin(t, srv, http.MethodGet, "/api/people")
	resp.Body.Close()
	deadline, ok := people.gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > lookupTimeout {
		t.Fatalf("expected bounded context, deadline %v", deadline)
	}
}

func TestListPeopleWrongMethod(t *testing.T) {
	srv, _ := newAdminTestServer(t, &fakePeople{}, nil)
	defer srv.Close()

	resp := do(t, srv, http.MethodPost, "/api/people")
	code, _ := errorBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed || code != "method_not_allowed" {
		t.Fatalf("status %d code %q", resp.StatusCode, code)
	}
	allow := resp.Header.Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "HEAD") {
		t.Fatalf("Allow %q", allow)
	}
}

func TestListPeopleHead(t *testing.T) {
	people := &fakePeople{page: domain.PeoplePage{People: []domain.Person{}}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()
	resp := doAdmin(t, srv, http.MethodHead, "/api/people")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !people.called {
		t.Fatal("service not called")
	}
}

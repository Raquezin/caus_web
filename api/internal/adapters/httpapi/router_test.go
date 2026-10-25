package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type fakePeople struct {
	person  domain.Person
	page    domain.PeoplePage
	err     error
	called  bool
	gotID   uuid.UUID
	gotCtx  context.Context
	gotPage domain.Page
}

func (f *fakePeople) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	f.called = true
	f.gotID = id
	f.gotCtx = ctx
	return f.person, f.err
}

func (f *fakePeople) List(ctx context.Context, page domain.Page) (domain.PeoplePage, error) {
	f.called = true
	f.gotPage = page
	f.gotCtx = ctx
	return f.page, f.err
}

func newTestServer(people PeopleService, ping func(context.Context) error) (*httptest.Server, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	srv := httptest.NewServer(NewRouter(people, ping, log))
	return srv, &buf
}

func newAdminTestServer(t *testing.T, people PeopleService, ping func(context.Context) error) (*httptest.Server, *bytes.Buffer) {
	return newRoleTestServer(t, people, ping, "admin")
}

func newRoleTestServer(t *testing.T, people PeopleService, ping func(context.Context) error, role string) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), FirstName: "Ada", LastName: "Admin", Role: role},
	}, "https://people.test")
	srv := httptest.NewServer(NewRouter(people, ping, log, auth))
	return srv, &buf
}

func do(t *testing.T, srv *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func doAdmin(t *testing.T, srv *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func errorBody(t *testing.T, resp *http.Response) (code, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("invalid error JSON: %v", err)
	}
	resp.Body.Close()
	return body.Error.Code, body.Error.Message
}

func TestGetPersonSuccess(t *testing.T) {
	id := uuid.New()
	people := &fakePeople{person: domain.Person{ID: id, FirstName: "Ada", LastName: "Lovelace"}}
	srv, _ := newAdminTestServer(t, people, func(context.Context) error { return nil })
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people/"+id.String())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 3 {
		t.Fatalf("expected exactly 3 keys, got %v", body)
	}
	if body["id"] != id.String() || body["first_name"] != "Ada" || body["last_name"] != "Lovelace" {
		t.Fatalf("unexpected body %v", body)
	}
	for _, private := range []string{"email", "google_sub", "role", "created_at", "updated_at"} {
		if _, ok := body[private]; ok {
			t.Fatalf("private field %q exposed", private)
		}
	}
	if !people.called || people.gotID != id {
		t.Fatal("service not called with parsed id")
	}
}

func TestGetPersonInvalidID(t *testing.T) {
	people := &fakePeople{}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people/not-a-uuid")
	code, _ := errorBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest || code != "invalid_id" {
		t.Fatalf("status %d code %q", resp.StatusCode, code)
	}
	if people.called {
		t.Fatal("use case called for malformed id")
	}
}

func TestGetPersonNotFound(t *testing.T) {
	for _, err := range []error{
		domain.ErrPersonNotFound,
		fmt.Errorf("query: %w", domain.ErrPersonNotFound),
	} {
		people := &fakePeople{err: err}
		srv, _ := newAdminTestServer(t, people, nil)
		resp := doAdmin(t, srv, http.MethodGet, "/api/people/"+uuid.NewString())
		code, _ := errorBody(t, resp)
		srv.Close()
		if resp.StatusCode != http.StatusNotFound || code != "person_not_found" {
			t.Fatalf("status %d code %q", resp.StatusCode, code)
		}
	}
}

func TestGetPersonInternalErrorSanitized(t *testing.T) {
	people := &fakePeople{err: errors.New("password=SECRET-MARKER db exploded")}
	srv, logBuf := newAdminTestServer(t, people, nil)
	defer srv.Close()

	resp := doAdmin(t, srv, http.MethodGet, "/api/people/"+uuid.NewString())
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
	code, msg := "", ""
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	code, msg = parsed.Error.Code, parsed.Error.Message
	if code != "internal_error" || strings.Contains(msg, "SECRET") {
		t.Fatalf("code %q msg %q", code, msg)
	}
}

func TestUnknownRoute(t *testing.T) {
	srv, _ := newTestServer(&fakePeople{}, nil)
	defer srv.Close()
	for _, path := range []string{"/nope", "/api/people/", "/api/unknown"} {
		resp := do(t, srv, http.MethodGet, path)
		code, _ := errorBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		if code != "not_found" {
			t.Fatalf("%s: code %q", path, code)
		}
	}
}

func TestWrongMethod(t *testing.T) {
	srv, _ := newTestServer(&fakePeople{}, nil)
	defer srv.Close()

	resp := do(t, srv, http.MethodPost, "/api/people/"+uuid.NewString())
	code, _ := errorBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed || code != "method_not_allowed" {
		t.Fatalf("status %d code %q", resp.StatusCode, code)
	}
	allow := resp.Header.Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "HEAD") {
		t.Fatalf("Allow %q", allow)
	}
}

func TestHeadAllowed(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	srv, _ := newAdminTestServer(t, people, nil)
	defer srv.Close()
	resp := doAdmin(t, srv, http.MethodHead, "/api/people/"+people.person.ID.String())
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

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
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type fakePeople struct {
	person domain.Person
	err    error
	called bool
	gotID  uuid.UUID
	gotCtx context.Context
}

func (f *fakePeople) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	f.called = true
	f.gotID = id
	f.gotCtx = ctx
	return f.person, f.err
}

func newTestServer(people PeopleService, ping func(context.Context) error) (*httptest.Server, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	srv := httptest.NewServer(NewRouter(people, ping, log))
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
	srv, _ := newTestServer(people, func(context.Context) error { return nil })
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/api/people/"+id.String())
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
	srv, _ := newTestServer(people, nil)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/api/people/not-a-uuid")
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
		srv, _ := newTestServer(people, nil)
		resp := do(t, srv, http.MethodGet, "/api/people/"+uuid.NewString())
		code, _ := errorBody(t, resp)
		srv.Close()
		if resp.StatusCode != http.StatusNotFound || code != "person_not_found" {
			t.Fatalf("status %d code %q", resp.StatusCode, code)
		}
	}
}

func TestGetPersonInternalErrorSanitized(t *testing.T) {
	people := &fakePeople{err: errors.New("password=SECRET-MARKER db exploded")}
	srv, logBuf := newTestServer(people, nil)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/api/people/"+uuid.NewString())
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
	srv, _ := newTestServer(people, nil)
	defer srv.Close()
	resp := do(t, srv, http.MethodHead, "/api/people/"+people.person.ID.String())
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(&fakePeople{}, nil)
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	var gotCtx context.Context
	srv, _ := newTestServer(&fakePeople{}, func(ctx context.Context) error {
		gotCtx = ctx
		return nil
	})
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/readyz")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	deadline, ok := gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > readyTimeout {
		t.Fatalf("expected bounded deadline within %v, got %v", readyTimeout, deadline)
	}
}

func TestReadyzFailure(t *testing.T) {
	srv, logBuf := newTestServer(&fakePeople{}, func(context.Context) error {
		return errors.New("password=SECRET-MARKER refused")
	})
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/readyz")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "SECRET-MARKER") || strings.Contains(string(body), "refused") {
		t.Fatalf("details leaked: %s", body)
	}
	if strings.Contains(logBuf.String(), "SECRET-MARKER") {
		t.Fatalf("secret leaked in logs: %s", logBuf.String())
	}
}

func TestLookupContextHasDeadline(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	srv, _ := newTestServer(people, nil)
	defer srv.Close()
	resp := do(t, srv, http.MethodGet, "/api/people/"+people.person.ID.String())
	resp.Body.Close()
	deadline, ok := people.gotCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > lookupTimeout {
		t.Fatalf("expected bounded context, deadline %v", deadline)
	}
}

func TestContextPropagatedWithCancellation(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	router := NewRouter(people, func(context.Context) error { return nil }, log)

	type ctxKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "marker"))
	req := httptest.NewRequest(http.MethodGet, "/api/people/"+people.person.ID.String(), nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	cancel()
	router.ServeHTTP(rec, req)

	if !people.called {
		t.Fatal("service not called")
	}
	if people.gotCtx.Value(ctxKey{}) != "marker" {
		t.Fatal("context value not propagated")
	}
	if !errors.Is(people.gotCtx.Err(), context.Canceled) {
		t.Fatalf("derived context err %v, want canceled", people.gotCtx.Err())
	}
	if _, ok := people.gotCtx.Deadline(); !ok {
		t.Fatal("derived context missing deadline")
	}
}

func TestPanicRecovery(t *testing.T) {
	people := &panicPeople{}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	router := NewRouter(people, func(context.Context) error { return nil }, log)

	req := httptest.NewRequest(http.MethodGet, "/api/people/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers on panic response")
	}
	if strings.Contains(rec.Body.String(), "panic") || strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("panic details leaked: %s", rec.Body.String())
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error.Code != "internal_error" {
		t.Fatalf("code %q", parsed.Error.Code)
	}
	var requestLogs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if entry["msg"] == "request" {
			requestLogs = append(requestLogs, entry)
		}
	}
	if len(requestLogs) != 1 {
		t.Fatalf("expected exactly one request log, got %d: %s", len(requestLogs), buf.String())
	}
	if requestLogs[0]["status"] != float64(500) || requestLogs[0]["path"] != "/api/people/{id}" {
		t.Fatalf("request log %v", requestLogs[0])
	}
}

type panicPeople struct{}

func (panicPeople) GetByID(context.Context, uuid.UUID) (domain.Person, error) {
	panic("SECRET-MARKER boom")
}

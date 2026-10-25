package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func newTestRouter(t *testing.T, people PeopleService, limiter *rateLimiter) http.Handler {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	auth := newStubBackendAuth(t, &stubBackendAuth{
		account: domain.Account{ID: uuid.New(), Role: "admin"},
	}, "https://people.test")
	return newRouter(people, func(ctx context.Context) error { return nil }, log, limiter, auth)
}

func TestCORSAllowedOrigins(t *testing.T) {
	for _, origin := range []string{
		"https://people.test",
	} {
		people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
		router := newTestRouter(t, people, newRateLimiter(time.Now))
		req := sessionRequest(http.MethodGet, "/api/people/"+people.person.ID.String())
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", origin, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("%s: allow-origin %q", origin, got)
		}
		if expose := rec.Header().Get("Access-Control-Expose-Headers"); expose != "Retry-After, X-Request-ID" {
			t.Fatalf("%s: expose %q", origin, expose)
		}
		if vary := rec.Header().Values("Vary"); !contains(vary, "Origin") {
			t.Fatalf("%s: vary %v", origin, vary)
		}
	}
}

func TestCORSDeniedOrigins(t *testing.T) {
	for _, origin := range []string{
		"https://clubalgoritmiaus.es",
		"http://localhost:1",
		"http://localhost:8080",
		"https://localhost:65535",
		"http://clubalgoritmiaus.es",
		"https://clubalgoritmiaus.es.evil.com",
		"https://evilclubalgoritmiaus.es",
		"https://sub.clubalgoritmiaus.es",
		"https://clubalgoritmiaus.es:8443",
		"http://localhost",
		"https://localhost",
		"http://localhost:0",
		"http://localhost:65536",
		"http://localhost:abc",
		"http://127.0.0.1:8080",
		"null",
		"http://user@localhost:8080",
		"http://localhost:8080/path",
		"http://localhost:8080?x=1",
		"http://localhost:8080#frag",
		"http://localhost:8080?",
		"http://localhost:8080#",
		"http://[localhost]:8080",
		"localhost:8080",
		"not a url",
	} {
		people := &fakePeople{person: domain.Person{ID: uuid.New()}}
		router := newTestRouter(t, people, newRateLimiter(time.Now))
		req := sessionRequest(http.MethodGet, "/api/people/"+uuid.NewString())
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%q: status %d, want 403", origin, rec.Code)
		}
		var parsed struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("%q: %v", origin, err)
		}
		if parsed.Error.Code != "cors_origin_denied" {
			t.Fatalf("%q: code %q", origin, parsed.Error.Code)
		}
		if people.called {
			t.Fatalf("%q: application called", origin)
		}
	}
}

func TestCORSNoOriginPasses(t *testing.T) {
	people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
	router := newTestRouter(t, people, newRateLimiter(time.Now))
	req := sessionRequest(http.MethodGet, "/api/people/"+people.person.ID.String())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected allow-origin header")
	}
	if !people.called {
		t.Fatal("application not called")
	}
}

func TestCORSPreflightAccepted(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		people := &fakePeople{}
		router := newTestRouter(t, people, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
		req.Header.Set("Origin", "https://people.test")
		req.Header.Set("Access-Control-Request-Method", method)
		req.Header.Set("Access-Control-Request-Headers", "accept, CONTENT-TYPE")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d body %s", method, rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://people.test" {
			t.Fatalf("%s: allow-origin %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, HEAD, OPTIONS" {
			t.Fatalf("%s: allow-methods %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Accept, Content-Type" {
			t.Fatalf("%s: allow-headers %q", method, got)
		}
		if got := rec.Header().Get("Access-Control-Max-Age"); got != "600" {
			t.Fatalf("%s: max-age %q", method, got)
		}
		vary := rec.Header().Values("Vary")
		for _, want := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
			if !contains(vary, want) {
				t.Fatalf("%s: vary %v missing %q", method, vary, want)
			}
		}
		if people.called {
			t.Fatalf("%s: application called for preflight", method)
		}
	}
}

func TestCORSPreflightUnsupportedMethod(t *testing.T) {
	router := newTestRouter(t, &fakePeople{}, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
	req.Header.Set("Origin", "https://people.test")
	req.Header.Set("Access-Control-Request-Method", "DELETE")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error.Code != "method_not_allowed" {
		t.Fatalf("code %q", parsed.Error.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD, OPTIONS" {
		t.Fatalf("Allow %q", allow)
	}
}

func TestCORSPreflightDeniedHeaders(t *testing.T) {
	for _, headers := range []string{"Authorization", "Accept, X-Api-Key", " Content-Type , x-csrf"} {
		router := newTestRouter(t, &fakePeople{}, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
		req.Header.Set("Origin", "https://people.test")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", headers)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d", headers, rec.Code)
		}
		var parsed struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Error.Code != "cors_headers_denied" {
			t.Fatalf("%q: code %q", headers, parsed.Error.Code)
		}
	}
}

func TestOptionsWithoutPreflightContinues(t *testing.T) {
	router := newTestRouter(t, &fakePeople{}, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodOptions, "/api/people/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestCORSHeadersOnErrors(t *testing.T) {
	origin := "https://people.test"
	router := newTestRouter(t, &fakePeople{err: errors.New("boom")}, newRateLimiter(time.Now))
	req := sessionRequest(http.MethodGet, "/api/people/"+uuid.NewString())
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("allow-origin %q", got)
	}
}

type staticPeople struct{ person domain.Person }

func (s staticPeople) GetByID(context.Context, uuid.UUID) (domain.Person, error) {
	return s.person, nil
}

func (s staticPeople) List(context.Context, domain.Page) (domain.PeoplePage, error) {
	return domain.PeoplePage{}, nil
}

func contains(values []string, want string) bool {
	for _, v := range values {
		for _, part := range strings.Split(v, ", ") {
			if part == want {
				return true
			}
		}
	}
	return false
}

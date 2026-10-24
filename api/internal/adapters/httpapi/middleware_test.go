package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func newTestRouter(people PeopleService, limiter *rateLimiter) http.Handler {
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	return newRouter(people, func(ctx context.Context) error { return nil }, log, limiter)
}

func TestCORSAllowedOrigins(t *testing.T) {
	for _, origin := range []string{
		"https://clubalgoritmiaus.es",
		"http://localhost:1",
		"http://localhost:8080",
		"https://localhost:65535",
	} {
		people := &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}
		router := newTestRouter(people, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodGet, "/api/people/"+people.person.ID.String(), nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", origin, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("%s: allow-origin %q", origin, got)
		}
		if expose := rec.Header().Get("Access-Control-Expose-Headers"); expose != "Retry-After" {
			t.Fatalf("%s: expose %q", origin, expose)
		}
		if vary := rec.Header().Values("Vary"); !contains(vary, "Origin") {
			t.Fatalf("%s: vary %v", origin, vary)
		}
	}
}

func TestCORSDeniedOrigins(t *testing.T) {
	for _, origin := range []string{
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
		router := newTestRouter(people, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodGet, "/api/people/"+uuid.NewString(), nil)
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
	router := newTestRouter(people, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodGet, "/api/people/"+people.person.ID.String(), nil)
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
		router := newTestRouter(people, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
		req.Header.Set("Origin", "https://clubalgoritmiaus.es")
		req.Header.Set("Access-Control-Request-Method", method)
		req.Header.Set("Access-Control-Request-Headers", "accept, CONTENT-TYPE")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d body %s", method, rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://clubalgoritmiaus.es" {
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
	router := newTestRouter(&fakePeople{}, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
	req.Header.Set("Origin", "https://clubalgoritmiaus.es")
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
		router := newTestRouter(&fakePeople{}, newRateLimiter(time.Now))
		req := httptest.NewRequest(http.MethodOptions, "/api/people", nil)
		req.Header.Set("Origin", "https://clubalgoritmiaus.es")
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
	router := newTestRouter(&fakePeople{}, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodOptions, "/api/people/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestCORSHeadersOnErrors(t *testing.T) {
	origin := "https://clubalgoritmiaus.es"
	router := newTestRouter(&fakePeople{err: errors.New("boom")}, newRateLimiter(time.Now))
	req := httptest.NewRequest(http.MethodGet, "/api/people/"+uuid.NewString(), nil)
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

func TestRateLimitExhaustionAndHeaders(t *testing.T) {
	now := time.Now()
	current := now
	limiter := newRateLimiter(func() time.Time { return current })
	router := newTestRouter(&fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)

	path := "/api/people/" + uuid.NewString()
	for i := 0; i < rateLimitRequests; i++ {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.1:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set("Origin", "https://clubalgoritmiaus.es")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", rec.Code)
	}
	retry := rec.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(retry)
	if err != nil || seconds < 1 {
		t.Fatalf("Retry-After %q", retry)
	}
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error.Code != "rate_limited" {
		t.Fatalf("code %q", parsed.Error.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://clubalgoritmiaus.es" {
		t.Fatalf("allow-origin on 429 %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers on 429")
	}

	current = now.Add(2 * time.Minute)
	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.1:1234"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("after reset: status %d", rec.Code)
	}
}

func TestRateLimitIPIsolationAndForgedHeaders(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(&fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)
	path := "/api/people/" + uuid.NewString()

	for i := 0; i < rateLimitRequests; i++ {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "198.51.100.5:9999"
		req.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i))
		req.Header.Set("Forwarded", "for=192.0.2."+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "198.51.100.5:9999"
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forged header evaded limit: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "198.51.100.6:9999"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("different IP limited: status %d", rec.Code)
	}
}

func TestRateLimitHealthProbesExempt(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(&fakePeople{}, limiter)
	for i := 0; i < rateLimitRequests+10; i++ {
		for _, path := range []string{"/api/healthz", "/api/readyz"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "192.0.2.1:1"
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s request %d: status %d", path, i, rec.Code)
			}
		}
	}
}

func TestRateLimitUnknownRoutesLimited(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(&fakePeople{}, limiter)
	for i := 0; i < rateLimitRequests; i++ {
		req := httptest.NewRequest(http.MethodGet, "/nope", nil)
		req.RemoteAddr = "192.0.2.2:1"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	req.RemoteAddr = "192.0.2.2:1"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRateLimitMalformedRemoteAddrSharesUnknownBucket(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	for i := 0; i < rateLimitRequests; i++ {
		if _, ok := limiter.allow(clientIP(&http.Request{RemoteAddr: "garbage"})); !ok {
			t.Fatalf("request %d rejected", i)
		}
	}
	if _, ok := limiter.allow(clientIP(&http.Request{RemoteAddr: "::not-an-ip::"})); ok {
		t.Fatal("malformed addresses did not share unknown bucket")
	}
	if addr := clientIP(&http.Request{RemoteAddr: "garbage"}); addr != (netip.Addr{}) {
		t.Fatalf("malformed addr mapped to %v", addr)
	}
	if addr := clientIP(&http.Request{RemoteAddr: "[::ffff:192.0.2.9]:80"}); !addr.Is4() {
		t.Fatalf("IPv4-mapped addr not unmapped: %v", addr)
	}
}

func TestRateLimitBoundedClients(t *testing.T) {
	current := time.Now()
	limiter := newRateLimiter(func() time.Time { return current })
	for i := 0; i < rateLimitMaxClients; i++ {
		addr := netip.AddrFrom4([4]byte{byte(i >> 16), byte(i >> 8), byte(i), 1})
		if _, ok := limiter.allow(addr); !ok {
			t.Fatalf("client %d rejected before cap", i)
		}
	}
	retry, ok := limiter.allow(netip.MustParseAddr("8.8.8.8"))
	if ok {
		t.Fatal("new client accepted at cap")
	}
	if retry < time.Second {
		t.Fatalf("retry after %v", retry)
	}
	current = current.Add(2 * time.Minute)
	if _, ok := limiter.allow(netip.MustParseAddr("8.8.8.8")); !ok {
		t.Fatal("new client still rejected after sweep")
	}
}

func TestRateLimitRetryAfterExactCeiling(t *testing.T) {
	start := time.Now()
	current := start
	limiter := newRateLimiter(func() time.Time { return current })
	router := newTestRouter(staticPeople{person: domain.Person{ID: uuid.New()}}, limiter)
	windowEnd := start.Add(rateLimitWindow)

	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/people/"+uuid.NewString(), nil)
		req.RemoteAddr = "203.0.113.7:5555"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < rateLimitRequests; i++ {
		if rec := request(); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	current = windowEnd.Add(-time.Second - time.Nanosecond)
	if rec := request(); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("status %d retry-after %q, want 429 and 2", rec.Code, rec.Header().Get("Retry-After"))
	}

	current = windowEnd.Add(-time.Nanosecond)
	if rec := request(); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("status %d retry-after %q, want 429 and 1", rec.Code, rec.Header().Get("Retry-After"))
	}

	current = windowEnd
	if rec := request(); rec.Code != http.StatusOK {
		t.Fatalf("at window end: status %d", rec.Code)
	}
}

func TestPreflightDoesNotConsumeRateLimit(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(staticPeople{person: domain.Person{ID: uuid.New()}}, limiter)
	path := "/api/people/" + uuid.NewString()

	for i := 0; i < rateLimitRequests; i++ {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.8:1"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	preflight := httptest.NewRequest(http.MethodOptions, path, nil)
	preflight.RemoteAddr = "203.0.113.8:1"
	preflight.Header.Set("Origin", "https://clubalgoritmiaus.es")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: status %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.8:1"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("real request: status %d", rec.Code)
	}
}

type staticPeople struct{ person domain.Person }

func (s staticPeople) GetByID(context.Context, uuid.UUID) (domain.Person, error) {
	return s.person, nil
}

func (s staticPeople) List(context.Context, domain.Page) (domain.PeoplePage, error) {
	return domain.PeoplePage{}, nil
}

func TestRateLimitConcurrentAccess(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(staticPeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)
	path := "/api/people/" + uuid.NewString()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.RemoteAddr = "10.0.0." + strconv.Itoa(n) + ":1"
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
			}
		}(i)
	}
	wg.Wait()
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

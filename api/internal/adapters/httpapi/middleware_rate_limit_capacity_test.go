package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestRateLimitBoundedClients(t *testing.T) {
	current := time.Now()
	limiter := newRateLimiter(func() time.Time { return current })
	for i := 0; i < rateLimitMaxClients; i++ {
		addr := netip.AddrFrom4([4]byte{byte(i >> 16), byte(i >> 8), byte(i), 1})
		if _, ok := limiter.allow(addr); !ok {
			t.Fatalf("client %d rejected before cap", i)
		}
	}
	if _, ok := limiter.allow(netip.MustParseAddr("8.8.8.8")); !ok {
		t.Fatal("new client rejected at cap")
	}
	if len(limiter.clients) != rateLimitMaxClients {
		t.Fatalf("client table holds %d entries, want %d", len(limiter.clients), rateLimitMaxClients)
	}
	current = current.Add(2 * time.Minute)
	if _, ok := limiter.allow(netip.MustParseAddr("8.8.8.8")); !ok {
		t.Fatal("new client still rejected after sweep")
	}
}

func TestRateLimitFullCapacityFreesExpiredSlotsBeforeNextSweep(t *testing.T) {
	start := time.Now()
	current := start
	limiter := newRateLimiter(func() time.Time { return current })
	newcomer := netip.MustParseAddr("8.8.8.8")
	if _, ok := limiter.allow(netip.MustParseAddr("9.9.9.9")); !ok {
		t.Fatal("initial client rejected")
	}

	current = start.Add(time.Second)
	for i := 1; i < rateLimitMaxClients; i++ {
		addr := netip.AddrFrom4([4]byte{byte(i >> 16), byte(i >> 8), byte(i), 1})
		if _, ok := limiter.allow(addr); !ok {
			t.Fatalf("client %d rejected before cap", i)
		}
	}

	current = start.Add(rateLimitWindow)
	retry, ok := limiter.allow(newcomer)
	if !ok {
		t.Fatal("newcomer rejected although the first slot expired")
	}
	if retry != 0 {
		t.Fatalf("retry after %v on accepted request", retry)
	}

	current = start.Add(rateLimitWindow + 500*time.Millisecond)
	retry, ok = limiter.allow(netip.MustParseAddr("8.8.4.4"))
	if !ok {
		t.Fatal("client rejected while every slot is active")
	}
	if retry != 0 {
		t.Fatalf("retry after %v on accepted request", retry)
	}
	if len(limiter.clients) != rateLimitMaxClients {
		t.Fatalf("client table holds %d entries, want %d", len(limiter.clients), rateLimitMaxClients)
	}

	current = start.Add(rateLimitWindow + time.Second)
	if _, ok := limiter.allow(netip.MustParseAddr("8.8.4.4")); !ok {
		t.Fatal("client rejected after earliest slots expired, before the periodic sweep")
	}
}

func TestRateLimitRetryAfterExactCeiling(t *testing.T) {
	start := time.Now()
	current := start
	limiter := newRateLimiter(func() time.Time { return current })
	router := newTestRouter(t, staticPeople{person: domain.Person{ID: uuid.New()}}, limiter)
	windowEnd := start.Add(rateLimitWindow)

	request := func() *httptest.ResponseRecorder {
		req := sessionRequest(http.MethodPost, "/api/people/"+uuid.NewString())
		req.RemoteAddr = "203.0.113.7:5555"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < rateLimitRequests; i++ {
		if rec := request(); rec.Code != http.StatusMethodNotAllowed {
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
	if rec := request(); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("at window end: status %d", rec.Code)
	}
}

func TestPreflightDoesNotConsumeRateLimit(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(t, staticPeople{person: domain.Person{ID: uuid.New()}}, limiter)
	path := "/api/people/" + uuid.NewString()

	for i := 0; i < rateLimitRequests; i++ {
		req := sessionRequest(http.MethodPost, path)
		req.RemoteAddr = "203.0.113.8:1"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	preflight := httptest.NewRequest(http.MethodOptions, path, nil)
	preflight.RemoteAddr = "203.0.113.8:1"
	preflight.Header.Set("Origin", "https://people.test")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: status %d", rec.Code)
	}

	req := sessionRequest(http.MethodPost, path)
	req.RemoteAddr = "203.0.113.8:1"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("real request: status %d", rec.Code)
	}
}

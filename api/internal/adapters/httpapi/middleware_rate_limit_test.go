package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestRateLimitExhaustionAndHeaders(t *testing.T) {
	now := time.Now()
	current := now
	limiter := newRateLimiter(func() time.Time { return current })
	router := newTestRouter(t, &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)

	path := "/api/people/" + uuid.NewString()
	for i := 0; i < rateLimitRequests; i++ {
		req := sessionRequest(http.MethodGet, path)
		req.RemoteAddr = "203.0.113.1:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	req := sessionRequest(http.MethodGet, path)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set("Origin", "https://people.test")
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
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://people.test" {
		t.Fatalf("allow-origin on 429 %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers on 429")
	}

	current = now.Add(2 * time.Minute)
	req = sessionRequest(http.MethodGet, path)
	req.RemoteAddr = "203.0.113.1:1234"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("after reset: status %d", rec.Code)
	}
}

func TestRateLimitIPIsolationAndForgedHeaders(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(t, &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)
	path := "/api/people/" + uuid.NewString()

	for i := 0; i < rateLimitRequests; i++ {
		req := sessionRequest(http.MethodGet, path)
		req.RemoteAddr = "198.51.100.5:9999"
		req.Header.Set("X-Forwarded-For", "10.0.0."+strconv.Itoa(i))
		req.Header.Set("Forwarded", "for=192.0.2."+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}

	req := sessionRequest(http.MethodGet, path)
	req.RemoteAddr = "198.51.100.5:9999"
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forged header evaded limit: status %d", rec.Code)
	}

	req = sessionRequest(http.MethodGet, path)
	req.RemoteAddr = "198.51.100.6:9999"
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("different IP limited: status %d", rec.Code)
	}
}

func TestRateLimitHealthProbesExempt(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(t, &fakePeople{}, limiter)
	for i := 0; i < rateLimitRequests+10; i++ {
		for _, path := range []string{"/api/healthz", "/api/readyz"} {
			req := sessionRequest(http.MethodGet, path)
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
	router := newTestRouter(t, &fakePeople{}, limiter)
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

func TestRateLimitConcurrentAccess(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	router := newTestRouter(t, staticPeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)
	path := "/api/people/" + uuid.NewString()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				req := sessionRequest(http.MethodGet, path)
				req.RemoteAddr = "10.0.0." + strconv.Itoa(n) + ":1"
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
			}
		}(i)
	}
	wg.Wait()
}

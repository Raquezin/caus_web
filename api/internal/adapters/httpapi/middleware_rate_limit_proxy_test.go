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

func TestClientIPForwardedForTrust(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		headers []string
		want    string
	}{
		{"loopback no header", "127.0.0.1:8080", nil, "127.0.0.1"},
		{"loopback single", "127.0.0.1:8080", []string{"203.0.113.7"}, "203.0.113.7"},
		{"loopback rightmost of list", "127.0.0.1:8080", []string{"10.0.0.1, 203.0.113.7"}, "203.0.113.7"},
		{"loopback spoofed leading ignored", "127.0.0.1:8080", []string{"9.9.9.9, 198.51.100.3, 203.0.113.7"}, "203.0.113.7"},
		{"loopback multi line rightmost", "127.0.0.1:8080", []string{"10.0.0.1", "203.0.113.7"}, "203.0.113.7"},
		{"loopback invalid rightmost falls back", "127.0.0.1:8080", []string{"203.0.113.7, garbage"}, "127.0.0.1"},
		{"loopback empty rightmost falls back", "127.0.0.1:8080", []string{"203.0.113.7, "}, "127.0.0.1"},
		{"loopback empty header falls back", "127.0.0.1:8080", []string{""}, "127.0.0.1"},
		{"loopback ip port forwarded falls back", "127.0.0.1:8080", []string{"203.0.113.7:443"}, "127.0.0.1"},
		{"loopback ipv6 zone forwarded falls back", "127.0.0.1:8080", []string{"fe80::1%eth0"}, "127.0.0.1"},
		{"loopback ipv6 forwarded", "127.0.0.1:8080", []string{"2001:db8::42"}, "2001:db8::42"},
		{"loopback ipv6 peer", "[::1]:8080", []string{"203.0.113.7"}, "203.0.113.7"},
		{"mapped loopback peer", "[::ffff:127.0.0.1]:8080", []string{"203.0.113.7"}, "203.0.113.7"},
		{"mapped forwarded unifies", "127.0.0.1:8080", []string{"::ffff:203.0.113.7"}, "203.0.113.7"},
		{"nonloopback ignores header", "203.0.113.1:9000", []string{"10.0.0.1"}, "203.0.113.1"},
		{"nonloopback ignores multi header", "203.0.113.1:9000", []string{"10.0.0.1", "10.0.0.2"}, "203.0.113.1"},
		{"private peer ignores header", "10.1.2.3:9000", []string{"203.0.113.7"}, "10.1.2.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &http.Request{RemoteAddr: tc.remote, Header: http.Header{}}
			for _, value := range tc.headers {
				req.Header.Add("X-Forwarded-For", value)
			}
			want, err := netip.ParseAddr(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if got := clientIP(req); got != want {
				t.Fatalf("clientIP = %v, want %v", got, want)
			}
		})
	}
}

func TestRateLimitLoopbackProxyPerClientBuckets(t *testing.T) {
	limiter := newRateLimiter(time.Now)
	path := "/api/people/" + uuid.NewString()
	router := newTestRouter(t, &fakePeople{person: domain.Person{ID: uuid.New(), FirstName: "a", LastName: "b"}}, limiter)
	send := func(xff string) int {
		req := sessionRequest(http.MethodGet, path)
		req.RemoteAddr = "127.0.0.1:9000"
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < rateLimitRequests; i++ {
		if status := send("203.0.113.7"); status != http.StatusOK {
			t.Fatalf("request %d: status %d", i, status)
		}
	}
	if status := send("9.9.9.9, 203.0.113.7"); status != http.StatusTooManyRequests {
		t.Fatalf("spoofed leading value evaded bucket: status %d", status)
	}
	if status := send("203.0.113.9"); status != http.StatusOK {
		t.Fatalf("distinct proxied client limited: status %d", status)
	}
}

package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

const (
	rateLimitRequests   = 60
	rateLimitWindow     = time.Minute
	rateLimitMaxClients = 10000
)

type clientWindow struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	clients   map[netip.Addr]*clientWindow
	now       func() time.Time
	lastSweep time.Time
}

func newRateLimiter(now func() time.Time) *rateLimiter {
	return &rateLimiter{clients: make(map[netip.Addr]*clientWindow), now: now}
}

func (l *rateLimiter) allow(addr netip.Addr) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= rateLimitWindow {
		l.lastSweep = now
		for ip, window := range l.clients {
			if !now.Before(window.reset) {
				delete(l.clients, ip)
			}
		}
	}
	window, ok := l.clients[addr]
	if !ok {
		if len(l.clients) >= rateLimitMaxClients {
			return rateLimitWindow, false
		}
		window = &clientWindow{reset: now.Add(rateLimitWindow)}
		l.clients[addr] = window
	}
	if !now.Before(window.reset) {
		window.count = 0
		window.reset = now.Add(rateLimitWindow)
	}
	window.count++
	if window.count > rateLimitRequests {
		return window.reset.Sub(now), false
	}
	return 0, true
}

func clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

func rateLimit(limiter *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/healthz" || r.URL.Path == "/api/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		retryAfter, ok := limiter.allow(clientIP(r))
		if !ok {
			seconds := int((retryAfter + time.Second - 1) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
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
	mu            sync.Mutex
	clients       map[netip.Addr]*clientWindow
	now           func() time.Time
	lastSweep     time.Time
	earliestReset time.Time
}

func newRateLimiter(now func() time.Time) *rateLimiter {
	return &rateLimiter{clients: make(map[netip.Addr]*clientWindow), now: now}
}

func (l *rateLimiter) allow(addr netip.Addr) (time.Duration, bool) {
	if addr.Is6() {
		prefix, _ := addr.Prefix(64)
		addr = prefix.Addr()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= rateLimitWindow {
		l.sweep(now)
	}
	window, ok := l.clients[addr]
	if !ok {
		if len(l.clients) >= rateLimitMaxClients && !now.Before(l.earliestReset) {
			l.sweep(now)
		}
		if len(l.clients) >= rateLimitMaxClients {
			for evicted := range l.clients {
				delete(l.clients, evicted)
				break
			}
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

func (l *rateLimiter) sweep(now time.Time) {
	l.lastSweep = now
	l.earliestReset = now.Add(rateLimitWindow)
	for ip, window := range l.clients {
		if !now.Before(window.reset) {
			delete(l.clients, ip)
		} else if window.reset.Before(l.earliestReset) {
			l.earliestReset = window.reset
		}
	}
}

func clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	peer = peer.Unmap()
	if !peer.IsLoopback() {
		return peer
	}
	forwarded := r.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return peer
	}
	line := forwarded[len(forwarded)-1]
	token := strings.TrimSpace(line[strings.LastIndex(line, ",")+1:])
	addr, err := netip.ParseAddr(token)
	if err != nil || addr.Zone() != "" {
		return peer
	}
	return addr.Unmap()
}

func rateLimit(limiter *rateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
		if readOnly && !strings.HasPrefix(r.URL.Path, "/api/auth/") {
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

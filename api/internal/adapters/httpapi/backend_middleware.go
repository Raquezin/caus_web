package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func withSecurityHeaders(publicOrigin string, next http.Handler) http.Handler {
	secure := strings.HasPrefix(publicOrigin, "https://")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func withAccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := &accountContext{}
		r = r.WithContext(context.WithValue(r.Context(), accountContextKey{}, state))
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		attributes := []any{
			"method", r.Method,
			"path", r.Pattern,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFromContext(r.Context()),
		}
		if state.set {
			attributes = append(attributes, "user_id", state.account.ID.String())
		}
		log.Info("request", attributes...)
	})
}

type methodDispatch struct {
	methods map[string]http.Handler
	allow   []string
}

func endpointMethods(methods map[string]http.Handler) http.Handler {
	allowed := make([]string, 0, len(methods)+1)
	for method := range methods {
		allowed = append(allowed, method)
	}
	if _, ok := methods[http.MethodGet]; ok {
		allowed = append(allowed, http.MethodHead)
	}
	sort.Strings(allowed)
	return &methodDispatch{methods: methods, allow: allowed}
}

func getOnly(next http.Handler) http.Handler {
	return endpointMethods(map[string]http.Handler{http.MethodGet: next})
}

func (d *methodDispatch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if handler, ok := d.methods[r.Method]; ok {
		handler.ServeHTTP(w, r)
		return
	}
	if r.Method == http.MethodHead {
		if handler, ok := d.methods[http.MethodGet]; ok {
			handler.ServeHTTP(w, r)
			return
		}
	}
	w.Header().Set("Allow", strings.Join(d.allow, ", "))
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

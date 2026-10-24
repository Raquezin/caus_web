package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type PeopleService interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error)
	List(ctx context.Context, page domain.Page) (domain.PeoplePage, error)
}

func NewRouter(people PeopleService, ping func(ctx context.Context) error, log *slog.Logger, auth ...*Auth) http.Handler {
	return newRouter(people, ping, log, newRateLimiter(time.Now), auth...)
}

func newRouter(people PeopleService, ping func(ctx context.Context) error, log *slog.Logger, limiter *rateLimiter, auth ...*Auth) http.Handler {
	mux := http.NewServeMux()
	h := &handler{people: people, ping: ping, log: log}
	mux.Handle("/api/people", getOnly(http.HandlerFunc(h.listPeople)))
	mux.Handle("/api/people/{id}", getOnly(http.HandlerFunc(h.getPerson)))
	mux.Handle("/api/healthz", getOnly(http.HandlerFunc(h.healthz)))
	mux.Handle("/api/readyz", getOnly(http.HandlerFunc(h.readyz)))
	if len(auth) > 0 && auth[0] != nil {
		mux.Handle("/api/auth/google/login", getOnly(http.HandlerFunc(auth[0].login)))
		if auth[0].service != nil {
			mux.Handle("/api/me", getOnly(http.HandlerFunc(auth[0].me)))
			mux.Handle("/api/auth/google/callback", getOnly(http.HandlerFunc(auth[0].callback)))
		}
	}
	mux.Handle("/", http.HandlerFunc(notFound))
	return securityHeaders(accessLog(log, recoverer(cors(rateLimit(limiter, mux)), log)))
}

func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "resource not found")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		log.Info("request",
			"method", r.Method,
			"path", r.Pattern,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic recovered", "path", r.Pattern)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

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
	var configured *Auth
	if len(auth) > 0 {
		configured = auth[0]
	}
	return buildRouter(people, ping, log, configured, nil, limiter)
}

func NewRouterWithBackend(people PeopleService, ping func(ctx context.Context) error, log *slog.Logger, auth *Auth, backend *Backend) http.Handler {
	return buildRouter(people, ping, log, auth, backend, newRateLimiter(time.Now))
}

func buildRouter(people PeopleService, ping func(ctx context.Context) error, log *slog.Logger, auth *Auth, backend *Backend, limiter *rateLimiter) http.Handler {
	mux := http.NewServeMux()
	h := &handler{people: people, ping: ping, log: log}
	mux.Handle("/api/people", getOnly(auth.requireAdmin(http.HandlerFunc(h.listPeople))))
	mux.Handle("/api/people/{id}", getOnly(auth.requireAdmin(http.HandlerFunc(h.getPerson))))
	mux.Handle("/api/healthz", getOnly(http.HandlerFunc(h.healthz)))
	mux.Handle("/api/readyz", getOnly(http.HandlerFunc(h.readyz)))
	publicOrigin := ""
	if auth != nil {
		publicOrigin = auth.cfg.PublicURL
		mux.Handle("/api/auth/google/login", getOnly(http.HandlerFunc(auth.login)))
		if auth.service != nil {
			mux.Handle("/api/auth/google/callback", getOnly(http.HandlerFunc(auth.callback)))
		}
	}
	if backend != nil {
		backend.registerRoutes(mux, auth)
	}
	mux.Handle("/", http.HandlerFunc(notFound))
	return withRequestID(withSecurityHeaders(publicOrigin, withAccessLog(log, recoverer(cors(rateLimit(limiter, mux), publicOrigin, mux), log))))
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

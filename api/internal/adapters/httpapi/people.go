package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const lookupTimeout = 5 * time.Second
const readyTimeout = 2 * time.Second

type handler struct {
	people PeopleService
	ping   func(ctx context.Context) error
	log    *slog.Logger
}

type personResponse struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
}

func (h *handler) getPerson(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid person id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	person, err := h.people.GetByID(ctx, id)
	if errors.Is(err, domain.ErrPersonNotFound) {
		writeError(w, http.StatusNotFound, "person_not_found", "person not found")
		return
	}
	if err != nil {
		h.logError(r, "get person", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, personResponse{
		ID:        person.ID,
		FirstName: person.FirstName,
		LastName:  person.LastName,
	})
}

func (h *handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if err := h.ping(ctx); err != nil {
		h.logError(r, "readiness check", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "service unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) logError(r *http.Request, op string, err error) {
	h.log.Error(op+" failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
}

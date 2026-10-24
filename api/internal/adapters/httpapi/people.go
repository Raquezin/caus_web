package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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

func (h *handler) listPeople(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	result, err := h.people.List(ctx, page)
	if errors.Is(err, domain.ErrInvalidPagination) {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	if err != nil {
		h.logError(r, "list people", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	people := make([]personResponse, 0, len(result.People))
	for _, person := range result.People {
		people = append(people, personResponse{
			ID:        person.ID,
			FirstName: person.FirstName,
			LastName:  person.LastName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": people,
		"pagination": map[string]any{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"has_more": result.HasMore,
		},
	})
}

func parsePage(r *http.Request) (domain.Page, bool) {
	page := domain.Page{Limit: domain.DefaultPageLimit}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return domain.Page{}, false
	}
	if values, ok := query["limit"]; ok {
		if len(values) != 1 {
			return domain.Page{}, false
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > domain.MaxPageLimit {
			return domain.Page{}, false
		}
		page.Limit = limit
	}
	if values, ok := query["offset"]; ok {
		if len(values) != 1 {
			return domain.Page{}, false
		}
		offset, err := strconv.Atoi(values[0])
		if err != nil || offset < 0 {
			return domain.Page{}, false
		}
		page.Offset = offset
	}
	return page, true
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

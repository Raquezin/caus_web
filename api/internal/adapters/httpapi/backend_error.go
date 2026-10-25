package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"caus-platform/api/internal/domain"
)

func (b *Backend) backendError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	var validation domain.ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": map[string]any{
				"code":    "validation_failed",
				"message": "validation failed",
				"fields":  validation.Fields,
			},
		})
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "resource conflict")
	case errors.Is(err, domain.ErrInvalidPagination):
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
	default:
		if b != nil && b.log != nil {
			b.log.Error("backend request failed",
				"operation", r.Pattern,
				"error_type", fmt.Sprintf("%T", err),
			)
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

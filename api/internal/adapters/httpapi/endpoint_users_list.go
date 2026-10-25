package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) listUsers(w http.ResponseWriter, r *http.Request) {
	search, ok := parseUserQuery(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "invalid search query")
		return
	}
	page, ok := parsePage(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	result, err := b.service.ListUsers(ctx, search, page)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": userResponses(result.Users),
		"pagination": map[string]any{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"has_more": result.HasMore,
		},
	})
}

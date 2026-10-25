package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) listCompetitions(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	result, err := b.service.ListCompetitions(ctx, page)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": competitionResponses(result.Competitions),
		"pagination": map[string]any{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"has_more": result.HasMore,
		},
	})
}

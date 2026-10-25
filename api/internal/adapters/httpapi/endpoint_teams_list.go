package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) listTeams(w http.ResponseWriter, r *http.Request) {
	competitionID, ok := requestUUID(w, r)
	if !ok {
		return
	}
	page, ok := parsePage(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	result, err := b.service.ListTeams(ctx, competitionID, page)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": teamResponses(result.Teams),
		"pagination": map[string]any{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"has_more": result.HasMore,
		},
	})
}

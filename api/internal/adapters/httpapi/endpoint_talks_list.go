package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) listTalks(w http.ResponseWriter, r *http.Request) {
	page, ok := parsePage(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
		return
	}
	filter, ok := parseTalkFilter(r, false)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_filter", "invalid filter")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	result, err := b.service.ListTalks(ctx, page, filter)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": talkResponses(result.Talks),
		"pagination": map[string]any{
			"limit":    page.Limit,
			"offset":   page.Offset,
			"has_more": result.HasMore,
		},
	})
}

package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) getCompetition(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	detail, err := b.service.GetCompetition(ctx, id)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, competitionDetailResponse(detail))
}

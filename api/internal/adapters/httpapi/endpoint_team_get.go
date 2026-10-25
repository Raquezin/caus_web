package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) getTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	team, err := b.service.GetTeam(ctx, id)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, teamResponse(team))
}

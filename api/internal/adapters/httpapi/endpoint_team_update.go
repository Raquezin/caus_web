package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) updateTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	var request teamPatchRequest
	if !decodeBody(w, r, &request) {
		return
	}
	patch, err := request.patch()
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	team, err := b.service.UpdateTeam(ctx, id, patch)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, teamResponse(team))
}

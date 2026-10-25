package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) createTeam(w http.ResponseWriter, r *http.Request) {
	competitionID, ok := requestUUID(w, r)
	if !ok {
		return
	}
	var request teamRequest
	if !decodeBody(w, r, &request) {
		return
	}
	input, err := request.input()
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	team, err := b.service.CreateTeam(ctx, competitionID, input)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/teams/"+team.ID.String())
	writeJSON(w, http.StatusCreated, teamResponse(team))
}

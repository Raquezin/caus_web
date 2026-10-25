package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) createCompetition(w http.ResponseWriter, r *http.Request) {
	var request competitionRequest
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
	competition, err := b.service.CreateCompetition(ctx, input)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/competitions/"+competition.ID.String())
	writeJSON(w, http.StatusCreated, competitionResponse(competition))
}

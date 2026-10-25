package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) createTalk(w http.ResponseWriter, r *http.Request) {
	var request talkRequest
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
	talk, err := b.service.CreateTalk(ctx, input)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/talks/"+talk.ID.String())
	writeJSON(w, http.StatusCreated, talkResponse(talk))
}

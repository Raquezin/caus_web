package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) updateTalk(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	var request talkPatchRequest
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
	talk, err := b.service.UpdateTalk(ctx, id, patch)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, talkResponse(talk))
}

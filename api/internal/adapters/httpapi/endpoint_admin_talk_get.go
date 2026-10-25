package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) adminGetTalk(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	talk, err := b.service.GetTalk(ctx, id, true)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, talkResponse(talk))
}

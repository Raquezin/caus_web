package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) deleteTalk(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	if err := b.service.DeleteTalk(ctx, id); err != nil {
		b.backendError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) deleteCompetition(w http.ResponseWriter, r *http.Request) {
	id, ok := requestUUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	if err := b.service.DeleteCompetition(ctx, id); err != nil {
		b.backendError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

package httpapi

import (
	"context"
	"net/http"
)

func (b *Backend) updateAccount(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	var request accountPatchRequest
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
	updated, err := b.service.UpdateAccount(ctx, account.ID, patch)
	if err != nil {
		b.backendError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, accountResponse(updated))
}

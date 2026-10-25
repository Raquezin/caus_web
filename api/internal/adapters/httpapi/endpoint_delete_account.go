package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (b *Backend) deleteAccount(w http.ResponseWriter, r *http.Request) {
	account, ok := accountFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	if err := b.service.DeleteAccount(ctx, account.ID); err != nil {
		b.backendError(w, r, err)
		return
	}
	if b.auth != nil {
		http.SetCookie(w, b.auth.sessionCookie("", -1, time.Unix(1, 0)))
	}
	w.WriteHeader(http.StatusNoContent)
}

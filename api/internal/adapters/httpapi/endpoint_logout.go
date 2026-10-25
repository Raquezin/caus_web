package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"caus-platform/api/internal/domain"
)

func (b *Backend) logout(w http.ResponseWriter, r *http.Request) {
	if b == nil || b.auth == nil || b.auth.service == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	token, ok := sessionToken(r, b.auth.cookieName("caus_session"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	if err := b.auth.service.Logout(ctx, token); err != nil {
		if errors.Is(err, domain.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		b.auth.internalError(w, r, err)
		return
	}
	http.SetCookie(w, b.auth.sessionCookie("", -1, time.Unix(1, 0)))
	w.WriteHeader(http.StatusNoContent)
}

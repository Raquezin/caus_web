package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"caus-platform/api/internal/domain"
)

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || origins[0] != a.cfg.PublicURL {
		writeError(w, http.StatusForbidden, "origin_denied", "origin not allowed")
		return
	}
	cookies := r.CookiesNamed("caus_session")
	if len(cookies) != 1 || cookies[0].Value == "" {
		http.SetCookie(w, a.sessionCookie("", -1, time.Unix(1, 0)))
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
	defer cancel()
	err := a.service.Logout(ctx, cookies[0].Value)
	if err != nil && !errors.Is(err, domain.ErrUnauthenticated) {
		a.log.Error("logout failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	http.SetCookie(w, a.sessionCookie("", -1, time.Unix(1, 0)))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "not signed in")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

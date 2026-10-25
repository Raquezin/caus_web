package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"caus-platform/api/internal/domain"
)

type accountContextKey struct{}

type accountContext struct {
	account domain.Account
	set     bool
}

func accountFromContext(ctx context.Context) (domain.Account, bool) {
	state, ok := ctx.Value(accountContextKey{}).(*accountContext)
	if !ok || !state.set {
		return domain.Account{}, false
	}
	return state.account, true
}

func requestWithAccount(r *http.Request, account domain.Account) *http.Request {
	if state, ok := r.Context().Value(accountContextKey{}).(*accountContext); ok {
		state.account = account
		state.set = true
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), accountContextKey{}, &accountContext{account: account, set: true}))
}

func sessionToken(r *http.Request, name string) (string, bool) {
	token := ""
	found := false
	for _, cookie := range r.Cookies() {
		if cookie.Name != name {
			continue
		}
		if found || cookie.Value == "" {
			return "", false
		}
		found = true
		token = cookie.Value
	}
	return token, found
}

func (a *Auth) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := accountFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := sessionToken(r, a.cookieName("caus_session"))
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
			return
		}
		if a == nil || a.service == nil {
			a.internalError(w, r, errors.New("authentication service unavailable"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), lookupTimeout)
		account, err := a.service.CurrentUser(ctx, token)
		cancel()
		if err != nil {
			if errors.Is(err, domain.ErrUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
				return
			}
			a.internalError(w, r, err)
			return
		}
		next.ServeHTTP(w, requestWithAccount(r, account))
	})
}

func (a *Auth) requireAdmin(next http.Handler) http.Handler {
	return a.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, ok := accountFromContext(r.Context())
		if !ok || account.Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (a *Auth) requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origins := r.Header.Values("Origin")
		if a == nil || a.cfg.PublicURL == "" || len(origins) != 1 || origins[0] != a.cfg.PublicURL {
			writeError(w, http.StatusForbidden, "origin_denied", "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Auth) internalError(w http.ResponseWriter, r *http.Request, err error) {
	if a != nil && a.log != nil {
		a.log.Error("authentication failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

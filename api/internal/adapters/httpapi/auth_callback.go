package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"caus-platform/api/internal/domain"
)

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.oauthCookie("", -1, time.Unix(1, 0)))
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "oauth_callback_invalid", "invalid OAuth callback")
		return
	}
	for _, values := range query {
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "oauth_callback_invalid", "invalid OAuth callback")
			return
		}
	}
	var cookie *http.Cookie
	for _, candidate := range r.Cookies() {
		if candidate.Name == "caus_oauth" {
			if cookie != nil {
				writeError(w, http.StatusBadRequest, "invalid_oauth_state", "invalid OAuth state")
				return
			}
			cookie = candidate
		}
	}
	if cookie == nil || query.Get("state") == "" {
		writeError(w, http.StatusBadRequest, "invalid_oauth_state", "invalid OAuth state")
		return
	}
	flow, err := decodeOAuthFlow(cookie.Value, time.Now())
	if err != nil || subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(flow.State)) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_oauth_state", "invalid OAuth state")
		return
	}
	if query.Has("error") {
		if query.Get("error") == "access_denied" && !query.Has("code") {
			writeError(w, http.StatusBadRequest, "oauth_denied", "OAuth authorization denied")
		} else {
			writeError(w, http.StatusBadRequest, "oauth_callback_invalid", "invalid OAuth callback")
		}
		return
	}
	if strings.TrimSpace(query.Get("code")) == "" {
		writeError(w, http.StatusBadRequest, "oauth_callback_invalid", "invalid OAuth callback")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	identity, err := a.provider.Exchange(ctx, query.Get("code"), flow.Verifier, flow.Nonce)
	if errors.Is(err, domain.ErrInvalidIdentity) {
		writeError(w, http.StatusUnauthorized, "invalid_identity", "invalid identity")
		return
	}
	if err != nil {
		a.callbackError(w, r, err)
		return
	}
	_, token, err := a.service.Login(ctx, identity)
	switch {
	case errors.Is(err, domain.ErrInvalidIdentity):
		writeError(w, http.StatusUnprocessableEntity, "invalid_identity", "invalid identity")
	case errors.Is(err, domain.ErrIdentityConflict):
		writeError(w, http.StatusConflict, "identity_conflict", "identity conflict")
	case err != nil:
		a.callbackError(w, r, err)
	default:
		maxAge := int(a.cfg.SessionTTL / time.Second)
		http.SetCookie(w, a.sessionCookie(token, maxAge, time.Now().Add(time.Duration(maxAge)*time.Second)))
		http.Redirect(w, r, flow.ReturnTo, http.StatusFound)
	}
}

func (a *Auth) sessionCookie(value string, maxAge int, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name: "caus_session", Value: value, Path: "/", MaxAge: maxAge, Expires: expires,
		HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode,
	}
}

func (a *Auth) callbackError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("OAuth callback failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

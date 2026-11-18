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
		if candidate.Name == a.cookieName("caus_oauth") {
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
			a.redirectSignInError(w, r, flow.ReturnTo, "oauth_denied")
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
		a.redirectSignInError(w, r, flow.ReturnTo, "invalid_identity")
		return
	}
	if err != nil {
		a.callbackError(w, r, flow.ReturnTo, err)
		return
	}
	_, token, err := a.service.Login(ctx, identity)
	switch {
	case errors.Is(err, domain.ErrInvalidIdentity):
		a.redirectSignInError(w, r, flow.ReturnTo, "invalid_identity")
	case errors.Is(err, domain.ErrIdentityConflict):
		a.redirectSignInError(w, r, flow.ReturnTo, "identity_conflict")
	case err != nil:
		a.callbackError(w, r, flow.ReturnTo, err)
	default:
		maxAge := int(a.cfg.SessionTTL / time.Second)
		http.SetCookie(w, a.sessionCookie(token, maxAge, time.Now().Add(time.Duration(maxAge)*time.Second)))
		w.Header().Set("Location", flow.ReturnTo)
		w.WriteHeader(http.StatusFound)
	}
}

func (a *Auth) sessionCookie(value string, maxAge int, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name: a.cookieName("caus_session"), Value: value, Path: "/", MaxAge: maxAge, Expires: expires,
		HttpOnly: true, Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode,
	}
}

func (a *Auth) redirectSignInError(w http.ResponseWriter, r *http.Request, returnTo, code string) {
	a.log.Info("sign-in rejected", "path", r.Pattern, "code", code)
	target, err := url.Parse(returnTo)
	if err != nil {
		target = &url.URL{Path: "/"}
	}
	values := target.Query()
	values.Set("auth_error", code)
	target.RawQuery = values.Encode()
	w.Header().Set("Location", target.String())
	w.WriteHeader(http.StatusFound)
}

func (a *Auth) callbackError(w http.ResponseWriter, r *http.Request, returnTo string, err error) {
	a.log.Error("OAuth callback failed", "path", r.Pattern, "error_type", fmt.Sprintf("%T", err))
	if errors.Is(err, domain.ErrAuthUnavailable) {
		a.redirectSignInError(w, r, returnTo, "auth_unavailable")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

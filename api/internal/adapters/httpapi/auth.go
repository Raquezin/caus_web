package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

const oauthFlowTTL = 10 * time.Minute
const maxOAuthCookieLength = 3800
const maxReturnToLength = 2048

type OAuthProvider interface {
	AuthorizationURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, nonce string) (domain.GoogleIdentity, error)
}

type AuthService interface {
	Login(ctx context.Context, identity domain.GoogleIdentity) (domain.Account, string, error)
	CurrentUser(ctx context.Context, token string) (domain.Account, error)
	Logout(ctx context.Context, token string) error
}

type AuthConfig struct {
	PublicURL  string
	SessionTTL time.Duration
}

type Auth struct {
	provider OAuthProvider
	service  AuthService
	cfg      AuthConfig
	log      *slog.Logger
}

type oauthFlow struct {
	State     string `json:"state"`
	Nonce     string `json:"nonce"`
	Verifier  string `json:"verifier"`
	ReturnTo  string `json:"return_to"`
	ExpiresAt int64  `json:"expires_at"`
}

func NewAuth(provider OAuthProvider, service AuthService, cfg AuthConfig, log *slog.Logger) *Auth {
	return &Auth{provider: provider, service: service, cfg: cfg, log: log}
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	returnTo := "/"
	values, present := query["return_to"]
	if present && len(values) == 1 {
		returnTo = values[0]
	}
	if err != nil || (present && len(values) != 1) || !validReturnTo(returnTo) {
		writeError(w, http.StatusBadRequest, "invalid_return_to", "invalid return path")
		return
	}
	flow := oauthFlow{
		State:     oauth2.GenerateVerifier(),
		Nonce:     oauth2.GenerateVerifier(),
		Verifier:  oauth2.GenerateVerifier(),
		ReturnTo:  returnTo,
		ExpiresAt: time.Now().Add(oauthFlowTTL).Unix(),
	}
	data, err := json.Marshal(flow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	value := base64.RawURLEncoding.EncodeToString(data)
	if len(value) > maxOAuthCookieLength {
		writeError(w, http.StatusBadRequest, "invalid_return_to", "invalid return path")
		return
	}
	http.SetCookie(w, a.oauthCookie(value, int(oauthFlowTTL/time.Second), time.Unix(flow.ExpiresAt, 0)))
	http.Redirect(w, r, a.provider.AuthorizationURL(flow.State, flow.Nonce, flow.Verifier), http.StatusFound)
}

func (a *Auth) oauthCookie(value string, maxAge int, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     "caus_oauth",
		Value:    value,
		Path:     "/api/auth/google",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.cfg.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
	}
}

func decodeOAuthFlow(value string, now time.Time) (oauthFlow, error) {
	if value == "" || len(value) > maxOAuthCookieLength {
		return oauthFlow{}, domain.ErrUnauthenticated
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return oauthFlow{}, domain.ErrUnauthenticated
	}
	var flow oauthFlow
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&flow); err != nil {
		return oauthFlow{}, domain.ErrUnauthenticated
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return oauthFlow{}, domain.ErrUnauthenticated
	}
	if !validOAuthRandom(flow.State) || !validOAuthRandom(flow.Nonce) || !validOAuthRandom(flow.Verifier) ||
		!validReturnTo(flow.ReturnTo) || flow.ExpiresAt <= now.Unix() || flow.ExpiresAt > now.Add(oauthFlowTTL).Unix() {
		return oauthFlow{}, domain.ErrUnauthenticated
	}
	return flow, nil
}

func validOAuthRandom(value string) bool {
	data, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(data) == 32
}

func validReturnTo(value string) bool {
	if len(value) > maxReturnToLength {
		return false
	}
	for {
		if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") ||
			strings.ContainsFunc(value, unicode.IsControl) {
			return false
		}
		u, err := url.Parse(value)
		if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Opaque != "" {
			return false
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return false
		}
		if decoded == value {
			return true
		}
		value = decoded
	}
}

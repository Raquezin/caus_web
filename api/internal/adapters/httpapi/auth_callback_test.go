package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

type callbackProviderStub struct {
	exchange func(context.Context, string, string, string) (domain.GoogleIdentity, error)
}

func (p callbackProviderStub) AuthorizationURL(state, nonce, verifier string) string { return "/" }
func (p callbackProviderStub) Exchange(ctx context.Context, code, verifier, nonce string) (domain.GoogleIdentity, error) {
	return p.exchange(ctx, code, verifier, nonce)
}

type callbackServiceStub struct {
	login func(context.Context, domain.GoogleIdentity) (domain.Account, string, error)
}

func (s callbackServiceStub) Login(ctx context.Context, identity domain.GoogleIdentity) (domain.Account, string, error) {
	return s.login(ctx, identity)
}
func (s callbackServiceStub) CurrentUser(context.Context, string) (domain.Account, error) {
	return domain.Account{}, domain.ErrUnauthenticated
}
func (s callbackServiceStub) Logout(context.Context, string) error {
	return domain.ErrUnauthenticated
}

func callbackTestFlow(t *testing.T) (*http.Cookie, oauthFlow) {
	t.Helper()
	flow := oauthFlow{
		State: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier(),
		ReturnTo: "/cuenta/", ExpiresAt: time.Now().Add(oauthFlowTTL).Unix(),
	}
	data, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "caus_oauth", Value: base64.RawURLEncoding.EncodeToString(data)}, flow
}

func TestGoogleCallbackValidation(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		cookies int
		code    string
	}{
		{name: "missing state", query: "?code=x", cookies: 1, code: "invalid_oauth_state"},
		{name: "empty state", query: "?state=&code=x", cookies: 1, code: "invalid_oauth_state"},
		{name: "missing cookie", query: "?state=STATE&code=x", code: "invalid_oauth_state"},
		{name: "duplicate cookie", query: "?state=STATE&code=x", cookies: 2, code: "invalid_oauth_state"},
		{name: "missing code", query: "?state=STATE", cookies: 1, code: "oauth_callback_invalid"},
		{name: "empty code", query: "?state=STATE&code=", cookies: 1, code: "oauth_callback_invalid"},
		{name: "blank code", query: "?state=STATE&code=+", cookies: 1, code: "oauth_callback_invalid"},
		{name: "duplicate state", query: "?state=STATE&state=STATE&code=x", cookies: 1, code: "oauth_callback_invalid"},
		{name: "duplicate code", query: "?state=STATE&code=x&code=x", cookies: 1, code: "oauth_callback_invalid"},
		{name: "duplicate error", query: "?state=STATE&error=access_denied&error=access_denied", cookies: 1, code: "oauth_callback_invalid"},
		{name: "code and error", query: "?state=STATE&code=x&error=access_denied", cookies: 1, code: "oauth_callback_invalid"},
		{name: "empty error", query: "?state=STATE&error=", cookies: 1, code: "oauth_callback_invalid"},
		{name: "semicolon", query: "?state=STATE&code=x;y", cookies: 1, code: "oauth_callback_invalid"},
		{name: "invalid escape", query: "?state=STATE&code=%ZZ", cookies: 1, code: "oauth_callback_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cookie, flow := callbackTestFlow(t)
			calls := 0
			provider := callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
				calls++
				return domain.GoogleIdentity{}, nil
			}}
			service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
				calls++
				return domain.Account{}, "", nil
			}}
			logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
			auth := NewAuth(provider, service, AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour}, logger)
			request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback"+strings.ReplaceAll(tc.query, "STATE", flow.State), nil)
			for i := 0; i < tc.cookies; i++ {
				request.AddCookie(cookie)
			}
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
			assertAuthError(t, recorder.Result(), 400, tc.code)
			if calls != 0 {
				t.Fatal("invalid callback reached exchange or application")
			}
		})
	}
}

func TestGoogleCallbackSanitizedErrors(t *testing.T) {
	cases := []struct {
		name        string
		providerErr error
		serviceErr  error
		status      int
		code        string
	}{
		{name: "provider identity", providerErr: domain.ErrInvalidIdentity, status: 401, code: "invalid_identity"},
		{name: "provider internal", providerErr: errors.New("private code token email client-secret"), status: 500, code: "internal_error"},
		{name: "app identity", serviceErr: domain.ErrInvalidIdentity, status: 422, code: "invalid_identity"},
		{name: "app conflict", serviceErr: domain.ErrIdentityConflict, status: 409, code: "identity_conflict"},
		{name: "app internal", serviceErr: errors.New("private code token email client-secret"), status: 500, code: "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cookie, flow := callbackTestFlow(t)
			var exchangeCtx context.Context
			provider := callbackProviderStub{exchange: func(ctx context.Context, code, verifier, nonce string) (domain.GoogleIdentity, error) {
				exchangeCtx = ctx
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 10*time.Second || code != "private-code" || verifier != flow.Verifier || nonce != flow.Nonce {
					t.Fatal("exchange lost bounded context or flow secrets")
				}
				return domain.GoogleIdentity{Subject: "verified"}, tc.providerErr
			}}
			service := callbackServiceStub{login: func(ctx context.Context, identity domain.GoogleIdentity) (domain.Account, string, error) {
				if exchangeCtx != ctx || identity.Subject != "verified" {
					t.Fatal("database work lost verified identity or shared deadline")
				}
				return domain.Account{}, "", tc.serviceErr
			}}
			logs := &authLogSink{}
			logger := slog.New(slog.NewJSONHandler(logs, nil))
			auth := NewAuth(provider, service, AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour}, logger)
			request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+flow.State+"&code=private-code", nil)
			request.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
			assertAuthError(t, recorder.Result(), tc.status, tc.code)
			if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), flow.State) || strings.Contains(logs.String(), flow.Verifier) ||
				strings.Contains(recorder.Body.String(), "private") || (tc.status == 500 && !strings.Contains(logs.String(), "error_type")) {
				t.Fatal("error response/logging not sanitized")
			}
		})
	}
}

func TestGoogleCallbackMethodsAndSecureCookies(t *testing.T) {
	provider := callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
		return domain.GoogleIdentity{}, nil
	}}
	raw := oauth2.GenerateVerifier()
	service := callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
		return domain.Account{}, raw, nil
	}}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	auth := NewAuth(provider, service, AuthConfig{PublicURL: "https://clubalgoritmiaus.es", SessionTTL: time.Hour + time.Nanosecond}, logger)
	server := httptest.NewServer(NewRouter(nil, nil, logger, auth))
	defer server.Close()
	server.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		cookie, flow := callbackTestFlow(t)
		request, err := http.NewRequest(method, server.URL+"/api/auth/google/callback?state="+flow.State+"&code=test", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if method == http.MethodPost {
			if response.StatusCode != 405 || response.Header.Get("Allow") != "GET, HEAD" {
				t.Fatal("callback not guarded by getOnly")
			}
			continue
		}
		if response.StatusCode != 302 || response.Header.Get("Location") != "/cuenta/" || (method == http.MethodHead && len(body) != 0) {
			t.Fatal("GET/HEAD callback contract changed")
		}
		if len(response.Cookies()) != 2 {
			t.Fatal("callback must clear flow and create session cookies")
		}
		for _, cookie := range response.Cookies() {
			if !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("production cookies missing security flags")
			}
			if cookie.Name == "caus_session" && (cookie.Path != "/" || cookie.MaxAge != 3600 || cookie.Value != raw) {
				t.Fatal("session cookie TTL not conservatively rounded")
			}
			if cookie.Name == "caus_oauth" && (cookie.Path != "/api/auth/google" || cookie.MaxAge != -1 || !cookie.Expires.Before(time.Now())) {
				t.Fatal("secure OAuth cookie not cleared consistently")
			}
		}
	}
	loginOnly := NewAuth(provider, nil, auth.cfg, logger)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback", nil)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, loginOnly).ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatal("login-only router registered callback without service")
	}
}

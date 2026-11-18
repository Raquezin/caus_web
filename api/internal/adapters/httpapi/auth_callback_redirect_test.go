package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func callbackFlowCookie(t *testing.T, returnTo string) (*http.Cookie, oauthFlow) {
	t.Helper()
	cookie, flow := callbackTestFlow(t)
	flow.ReturnTo = returnTo
	data, err := json.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	cookie.Value = base64.RawURLEncoding.EncodeToString(data)
	return cookie, flow
}

func callbackDeniedRequest(flow oauthFlow) *http.Request {
	request := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?state="+flow.State+"&error=access_denied", nil)
	return request
}

func TestGoogleCallbackDeniedRedirectKeepsReturnToParts(t *testing.T) {
	cookie, flow := callbackFlowCookie(t, "/charlas/?page=2&auth_error=stale#top")
	auth := NewAuth(
		callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
			return domain.GoogleIdentity{}, nil
		}},
		callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
			return domain.Account{}, "", nil
		}},
		AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)
	request := callbackDeniedRequest(flow)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)), auth).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status %d, wanted 302", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/charlas/?auth_error=oauth_denied&page=2#top" {
		t.Fatalf("location %q", location)
	}
}

func TestGoogleCallbackConflictRedirectTargetsRoot(t *testing.T) {
	cookie, flow := callbackFlowCookie(t, "/")
	auth := NewAuth(
		callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
			return domain.GoogleIdentity{Subject: "verified"}, nil
		}},
		callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
			return domain.Account{}, "", domain.ErrIdentityConflict
		}},
		AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)), auth).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status %d, wanted 302", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/?auth_error=identity_conflict" {
		t.Fatalf("location %q", location)
	}
}

func TestGoogleCallbackUnavailableRedirectStillLogsError(t *testing.T) {
	cookie, flow := callbackFlowCookie(t, "/")
	logs := &authLogSink{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	auth := NewAuth(
		callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
			return domain.GoogleIdentity{}, fmt.Errorf("jwks: %w", domain.ErrAuthUnavailable)
		}},
		callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
			return domain.Account{}, "", nil
		}},
		AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour},
		logger,
	)
	request := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	assertSignInRedirect(t, recorder.Result(), flow.ReturnTo, "auth_unavailable")
	if !strings.Contains(logs.String(), "OAuth callback failed") || !strings.Contains(logs.String(), "error_type") {
		t.Fatal("unavailable callback lost its error log line")
	}
}

func TestGoogleCallbackInternalFailureStaysJSON(t *testing.T) {
	cookie, flow := callbackFlowCookie(t, "/cuenta/")
	auth := NewAuth(
		callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
			return domain.GoogleIdentity{}, errors.New("exchange blew up")
		}},
		callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
			return domain.Account{}, "", nil
		}},
		AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet,
		"/api/auth/google/callback?state="+flow.State+"&code=auth-code", nil)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)), auth).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	assertAuthError(t, response, http.StatusInternalServerError, "internal_error")
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("internal failure set Location %q", location)
	}
}

func TestGoogleCallbackRejectionLogHasCodeOnly(t *testing.T) {
	cookie, flow := callbackFlowCookie(t, "/cuenta/")
	logs := &authLogSink{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	auth := NewAuth(
		callbackProviderStub{exchange: func(context.Context, string, string, string) (domain.GoogleIdentity, error) {
			return domain.GoogleIdentity{}, nil
		}},
		callbackServiceStub{login: func(context.Context, domain.GoogleIdentity) (domain.Account, string, error) {
			return domain.Account{}, "session-token-secret", nil
		}},
		AuthConfig{PublicURL: "http://localhost:8080", SessionTTL: time.Hour},
		logger,
	)
	request := callbackDeniedRequest(flow)
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	NewRouter(nil, nil, logger, auth).ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status %d, wanted 302", response.StatusCode)
	}
	output := logs.String()
	if !strings.Contains(output, "sign-in rejected") || !strings.Contains(output, `"code":"oauth_denied"`) {
		t.Fatalf("rejection log missing code: %s", output)
	}
	for _, secret := range []string{"ada@example.com", "verified-subject", "session-token-secret", cookie.Value, flow.State} {
		if strings.Contains(output, secret) {
			t.Fatalf("rejection log leaked %q", secret)
		}
	}
}

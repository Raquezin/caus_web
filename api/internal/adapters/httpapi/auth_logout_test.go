package httpapi

import (
	"context"
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

	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

type logoutServiceStub struct {
	callbackServiceStub
	logout func(context.Context, string) error
}

func (s logoutServiceStub) Logout(ctx context.Context, token string) error {
	return s.logout(ctx, token)
}

func logoutTestAuth(publicURL string, logout func(context.Context, string) error) (*Auth, *authLogSink) {
	logs := &authLogSink{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	auth := NewAuth(nil, logoutServiceStub{logout: logout}, AuthConfig{PublicURL: publicURL}, logger)
	return auth, logs
}

func assertLogoutError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil || response.StatusCode != status || body.Error.Code != code || body.Error.Message == "" {
		t.Fatalf("status %d, body %q; wanted %d %q", response.StatusCode, data, status, code)
	}
}

func assertLogoutCookie(t *testing.T, response *http.Response, secure bool) {
	t.Helper()
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("wanted only a session deletion cookie, got %d cookies", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "caus_session" || cookie.Value != "" || cookie.MaxAge != -1 ||
		!cookie.Expires.Equal(time.Unix(1, 0)) || cookie.Path != "/" || cookie.Domain != "" ||
		!cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure != secure {
		t.Fatal("incorrect session deletion cookie")
	}
}

func TestLogoutOriginBeforeAuthentication(t *testing.T) {
	for _, publicURL := range []string{"http://localhost:8080", "https://clubalgoritmiaus.es"} {
		t.Run(publicURL, func(t *testing.T) {
			cases := []struct {
				name    string
				origins []string
			}{
				{name: "missing"},
				{name: "empty", origins: []string{""}},
				{name: "null", origins: []string{"null"}},
				{name: "foreign", origins: []string{"https://foreign.example"}},
				{name: "other localhost port", origins: []string{"http://localhost:8081"}},
				{name: "duplicate", origins: []string{publicURL, publicURL}},
				{name: "duplicate foreign", origins: []string{publicURL, "https://foreign.example"}},
				{name: "comma joined", origins: []string{publicURL + ", " + publicURL}},
				{name: "space joined", origins: []string{publicURL + " " + publicURL}},
				{name: "trailing slash", origins: []string{publicURL + "/"}},
				{name: "path", origins: []string{publicURL + "/logout"}},
				{name: "query", origins: []string{publicURL + "?logout"}},
				{name: "fragment", origins: []string{publicURL + "#logout"}},
				{name: "whitespace", origins: []string{" " + publicURL}},
				{name: "invalid URL", origins: []string{"http://[invalid"}},
			}
			for _, tc := range cases {
				for _, authenticated := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/session=%t", tc.name, authenticated), func(t *testing.T) {
						auth, _ := logoutTestAuth(publicURL, func(context.Context, string) error {
							t.Fatal("bad Origin reached auth service")
							return nil
						})
						request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
						for _, origin := range tc.origins {
							request.Header.Add("Origin", origin)
						}
						if authenticated {
							request.AddCookie(&http.Cookie{Name: "caus_session", Value: oauth2.GenerateVerifier()})
						}
						recorder := httptest.NewRecorder()
						auth.logout(recorder, request)
						assertLogoutError(t, recorder.Result(), 403, "origin_denied")
						if len(recorder.Result().Cookies()) != 0 {
							t.Fatal("bad Origin changed cookies")
						}
					})
				}
			}
		})
	}
}

func TestLogoutCookieValidation(t *testing.T) {
	raw := oauth2.GenerateVerifier()
	cases := []struct {
		name    string
		cookies []string
	}{
		{name: "missing"},
		{name: "empty", cookies: []string{"caus_session="}},
		{name: "duplicate", cookies: []string{"caus_session=" + raw + "; caus_session=" + raw}},
		{name: "duplicate headers", cookies: []string{"caus_session=" + raw, "caus_session=" + raw}},
		{name: "duplicate empty", cookies: []string{"caus_session=; caus_session=" + raw}},
		{name: "other cookie only", cookies: []string{"caus_oauth=not-a-session"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth, _ := logoutTestAuth("http://localhost:8080", func(context.Context, string) error {
				t.Fatal("ambiguous or missing cookie reached service")
				return nil
			})
			request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
			request.Header.Set("Origin", auth.cfg.PublicURL)
			for _, cookie := range tc.cookies {
				request.Header.Add("Cookie", cookie)
			}
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, auth.log, auth).ServeHTTP(recorder, request)
			assertLogoutError(t, recorder.Result(), 401, "unauthenticated")
			assertLogoutCookie(t, recorder.Result(), false)
		})
	}
}

func TestLogoutServiceResultsAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "success", status: 204},
		{name: "unauthenticated", err: fmt.Errorf("logout: %w", domain.ErrUnauthenticated), status: 401, code: "unauthenticated"},
		{name: "database failure", err: errors.New("private-database-cookie-email-token"), status: 500, code: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := oauth2.GenerateVerifier()
			var gotCtx context.Context
			calls := 0
			auth, logs := logoutTestAuth("https://clubalgoritmiaus.es", func(ctx context.Context, token string) error {
				calls++
				gotCtx = ctx
				deadline, ok := ctx.Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining < 4*time.Second || remaining > 5*time.Second || token != raw {
					t.Fatal("logout lost token or five-second deadline")
				}
				return tc.err
			})
			request := httptest.NewRequest(http.MethodPost, "/api/auth/logout?private-query", strings.NewReader("private-body"))
			request.Header.Set("Origin", auth.cfg.PublicURL)
			request.AddCookie(&http.Cookie{Name: "caus_session", Value: raw})
			request.AddCookie(&http.Cookie{Name: "unrelated", Value: "retained"})
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, auth.log, auth).ServeHTTP(recorder, request)
			if calls != 1 || gotCtx == nil || !errors.Is(gotCtx.Err(), context.Canceled) || request.Context().Err() != nil {
				t.Fatal("logout did not call once and release its own context")
			}
			if tc.status == 204 {
				if recorder.Code != 204 || recorder.Body.Len() != 0 {
					t.Fatal("successful logout must have no response body")
				}
			} else {
				assertLogoutError(t, recorder.Result(), tc.status, tc.code)
			}
			if tc.status == 500 {
				if len(recorder.Result().Cookies()) != 0 {
					t.Fatal("transient failure cleared a valid session")
				}
				var failure map[string]any
				firstLine := strings.Split(logs.String(), "\n")[0]
				if json.Unmarshal([]byte(firstLine), &failure) != nil || failure["msg"] != "logout failed" ||
					failure["path"] != "/api/auth/logout" || failure["error_type"] != "*errors.errorString" || len(failure) != 5 {
					t.Fatalf("unexpected sanitized failure log: %s", firstLine)
				}
			} else {
				assertLogoutCookie(t, recorder.Result(), true)
			}
			for _, secret := range []string{raw, "private-database", "private-query", "private-body"} {
				if strings.Contains(logs.String(), secret) || strings.Contains(recorder.Body.String(), secret) {
					t.Fatal("response or logs leaked sensitive data")
				}
			}
		})
	}
}

func TestLogoutContextPropagation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if canceled {
				cancel()
			}
			parentDeadline, _ := parent.Deadline()
			var gotCtx context.Context
			auth, _ := logoutTestAuth("http://localhost:8080", func(ctx context.Context, token string) error {
				gotCtx = ctx
				if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(parentDeadline) || ctx.Err() != parent.Err() {
					t.Fatal("logout lost parent deadline or cancellation")
				}
				if canceled {
					return ctx.Err()
				}
				return nil
			})
			request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil).WithContext(parent)
			request.Header.Set("Origin", auth.cfg.PublicURL)
			request.AddCookie(&http.Cookie{Name: "caus_session", Value: oauth2.GenerateVerifier()})
			recorder := httptest.NewRecorder()
			NewRouter(nil, nil, auth.log, auth).ServeHTTP(recorder, request)
			if gotCtx == nil || !errors.Is(gotCtx.Err(), context.Canceled) {
				t.Fatal("logout did not cancel its child context")
			}
			if canceled {
				assertLogoutError(t, recorder.Result(), 500, "internal_error")
				if len(recorder.Result().Cookies()) != 0 {
					t.Fatal("canceled request cleared cookie")
				}
			} else if recorder.Code != 204 || parent.Err() != nil {
				t.Fatal("logout canceled the parent context")
			}
		})
	}
}

func TestLogoutMethodsAndRegistration(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			auth, _ := logoutTestAuth("http://localhost:8080", func(context.Context, string) error {
				t.Fatal("non-POST reached service")
				return nil
			})
			server := httptest.NewServer(NewRouter(nil, nil, auth.log, auth))
			defer server.Close()
			request, err := http.NewRequest(method, server.URL+"/api/auth/logout", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(&http.Cookie{Name: "caus_session", Value: oauth2.GenerateVerifier()})
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 405 || response.Header.Get("Allow") != "POST" || len(response.Cookies()) != 0 {
				t.Fatal("non-POST was not rejected before cookie/auth checks")
			}
			if method == http.MethodHead {
				body, err := io.ReadAll(response.Body)
				if err != nil || len(body) != 0 {
					t.Fatal("HEAD returned a body")
				}
			} else {
				assertLogoutError(t, response, 405, "method_not_allowed")
			}
		})
	}
	auth, _ := logoutTestAuth("http://localhost:8080", nil)
	auth.service = nil
	for _, configured := range []*Auth{nil, auth} {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		recorder := httptest.NewRecorder()
		NewRouter(nil, nil, auth.log, configured).ServeHTTP(recorder, request)
		if recorder.Code != 404 {
			t.Fatal("logout registered without service")
		}
	}
}

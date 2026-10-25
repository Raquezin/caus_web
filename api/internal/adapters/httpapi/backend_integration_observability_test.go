package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestBackendCORSAndObservability(t *testing.T) {
	t.Run("preflight and write origins", func(t *testing.T) {
		f := newIntegrationFixture(t)
		status, header, body := f.do(integrationRequest{
			method:  http.MethodOptions,
			path:    "/api/admin/talks",
			origins: []string{f.origin},
			headers: map[string]string{
				"Access-Control-Request-Method":  http.MethodPost,
				"Access-Control-Request-Headers": "Accept, Content-Type",
			},
		})
		if status != http.StatusNoContent {
			t.Fatalf("write preflight: status %d body %s", status, body)
		}
		if header.Get("Access-Control-Allow-Origin") != f.origin || header.Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatalf("write preflight headers %v", header)
		}
		if methods := header.Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodPost) ||
			!strings.Contains(methods, http.MethodGet) || !strings.Contains(methods, http.MethodOptions) {
			t.Fatalf("write preflight methods %q", methods)
		}
		if header.Get("Access-Control-Max-Age") != "600" {
			t.Fatalf("write preflight max-age %q", header.Get("Access-Control-Max-Age"))
		}

		status, header, body = f.do(integrationRequest{
			method:  http.MethodOptions,
			path:    "/api/talks",
			origins: []string{f.origin},
			headers: map[string]string{"Access-Control-Request-Method": http.MethodDelete},
		})
		integrationExpect(t, status, body, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := header.Get("Allow"); allow != "GET, HEAD, OPTIONS" {
			t.Fatalf("read-only preflight Allow %q", allow)
		}

		status, _, body = f.do(integrationRequest{
			method:  http.MethodOptions,
			path:    "/api/talks",
			origins: []string{"https://evil.example"},
			headers: map[string]string{"Access-Control-Request-Method": http.MethodGet},
		})
		integrationExpect(t, status, body, http.StatusForbidden, "cors_origin_denied")

		status, header, body = f.do(integrationRequest{
			method: http.MethodGet, path: "/api/talks", origins: []string{f.origin},
		})
		if status != http.StatusOK {
			t.Fatalf("actual request: status %d body %s", status, body)
		}
		if header.Get("Access-Control-Allow-Origin") != f.origin ||
			header.Get("Access-Control-Allow-Credentials") != "true" ||
			header.Get("Access-Control-Expose-Headers") != "Retry-After, X-Request-ID" {
			t.Fatalf("actual response headers %v", header)
		}

		for i := 0; i < 60; i++ {
			status, _, body = f.do(integrationRequest{
				method:  http.MethodOptions,
				path:    "/api/talks",
				origins: []string{f.origin},
				headers: map[string]string{"Access-Control-Request-Method": http.MethodGet},
			})
			if status != http.StatusNoContent {
				t.Fatalf("preflight %d: status %d body %s", i, status, body)
			}
		}
		status, _, body = f.public(http.MethodGet, "/api/talks")
		if status != http.StatusOK {
			t.Fatalf("preflights consumed the rate limit: status %d body %s", status, body)
		}
	})

	t.Run("request instrumentation", func(t *testing.T) {
		f := newIntegrationFixture(t)
		f.logs.Reset()
		status, header, body := f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: f.adminToken})
		if status != http.StatusOK {
			t.Fatalf("/api/me: status %d body %s", status, body)
		}
		requestID := header.Get("X-Request-ID")
		if _, err := uuid.Parse(requestID); err != nil {
			t.Fatalf("request id %q", requestID)
		}
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Cache-Control") != "no-store" ||
			header.Get("Referrer-Policy") != "no-referrer" ||
			header.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("security headers %v", header)
		}
		if header.Get("Strict-Transport-Security") != "" {
			t.Fatalf("HSTS on http origin: %q", header.Get("Strict-Transport-Security"))
		}
		entries := f.logs.requestLogs(t)
		if len(entries) != 1 {
			t.Fatalf("request logs %v", entries)
		}
		entry := entries[0]
		if entry["request_id"] != requestID || entry["user_id"] != f.adminID.String() ||
			entry["status"] != float64(http.StatusOK) || entry["path"] != "/api/me" || entry["method"] != http.MethodGet {
			t.Fatalf("request log %v", entry)
		}
		if strings.Contains(f.logs.String(), f.adminToken) || strings.Contains(f.logs.String(), "admin@example.test") {
			t.Fatalf("log leaked session material: %s", f.logs.String())
		}

		f.logs.Reset()
		status, _, body = f.public(http.MethodGet, "/api/unknown?token=SECRET-MARKER")
		integrationExpect(t, status, body, http.StatusNotFound, "not_found")
		if strings.Contains(f.logs.String(), "SECRET-MARKER") {
			t.Fatalf("log leaked query values: %s", f.logs.String())
		}
		entries = f.logs.requestLogs(t)
		if len(entries) != 1 || entries[0]["path"] != "/" {
			t.Fatalf("unknown route log %v", entries)
		}

		status, _, body = f.do(integrationRequest{
			method: http.MethodPost, path: "/api/admin/talks", token: f.adminToken, origins: []string{f.origin},
			ctype: "application/json",
		})
		integrationExpect(t, status, body, http.StatusBadRequest, "invalid_body")
		status, _, body = f.admin(http.MethodPost, "/api/admin/talks", map[string]any{"unexpected": true})
		integrationExpect(t, status, body, http.StatusBadRequest, "invalid_body")
		status, _, body = f.do(integrationRequest{
			method: http.MethodPost, path: "/api/admin/talks", token: f.adminToken, origins: []string{f.origin},
			raw: []byte("{not-json"), ctype: "application/json",
		})
		integrationExpect(t, status, body, http.StatusBadRequest, "invalid_body")
		status, _, body = f.do(integrationRequest{
			method: http.MethodPost, path: "/api/admin/talks", token: f.adminToken, origins: []string{f.origin},
			raw: []byte("{}"), ctype: "text/plain",
		})
		integrationExpect(t, status, body, http.StatusUnsupportedMediaType, "unsupported_media_type")
		oversized := append([]byte(`{"title":"`), bytes.Repeat([]byte("a"), 70<<10)...)
		status, _, body = f.do(integrationRequest{
			method: http.MethodPost, path: "/api/admin/talks", token: f.adminToken, origins: []string{f.origin},
			raw: oversized, ctype: "application/json",
		})
		integrationExpect(t, status, body, http.StatusRequestEntityTooLarge, "request_too_large")
		for _, path := range []string{"/api/talks?limit=0", "/api/talks?offset=-1", "/api/talks?limit=a&limit=b"} {
			status, _, body = f.public(http.MethodGet, path)
			integrationExpect(t, status, body, http.StatusBadRequest, "invalid_pagination")
		}
		status, _, body = f.public(http.MethodGet, "/api/talks?period=week")
		integrationExpect(t, status, body, http.StatusBadRequest, "invalid_filter")
	})

	t.Run("panic response", func(t *testing.T) {
		var logs safeLogBuffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		auth := newStubBackendAuth(t, &stubBackendAuth{
			account: domain.Account{ID: uuid.New(), Role: "admin"},
		}, "https://panic.test")
		router := NewRouter(panicPeople{}, func(context.Context) error { return nil }, logger, auth)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/people/"+uuid.NewString()))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("panic status %d", recorder.Code)
		}
		if code := integrationErrorCode(t, recorder.Body.Bytes()); code != "internal_error" {
			t.Fatalf("panic code %q", code)
		}
		entries := logs.requestLogs(t)
		if len(entries) != 1 || entries[0]["status"] != float64(http.StatusInternalServerError) ||
			entries[0]["path"] != "/api/people/{id}" {
			t.Fatalf("panic request log %v", entries)
		}
	})
}

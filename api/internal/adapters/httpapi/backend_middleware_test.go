package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestWithRequestID(t *testing.T) {
	var seen string
	handler := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = requestIDFromContext(r.Context())
	}))
	previous := ""
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/talks", nil))
		header := recorder.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(header); err != nil {
			t.Fatalf("request id header %q", header)
		}
		if seen != header {
			t.Fatalf("context id %q, header %q", seen, header)
		}
		if header == previous {
			t.Fatal("request id reused")
		}
		previous = header
	}
}

func TestWithSecurityHeaders(t *testing.T) {
	for _, testCase := range []struct {
		publicURL string
		hsts      bool
	}{
		{"https://example.test", true},
		{"http://localhost:8080", false},
		{"", false},
	} {
		recorder := httptest.NewRecorder()
		withSecurityHeaders(testCase.publicURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		header := recorder.Header()
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Cache-Control") != "no-store" ||
			header.Get("Referrer-Policy") != "no-referrer" ||
			header.Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'none'" {
			t.Fatalf("%s headers %v", testCase.publicURL, header)
		}
		if hsts := header.Get("Strict-Transport-Security"); (hsts != "") != testCase.hsts {
			t.Fatalf("%s HSTS %q", testCase.publicURL, hsts)
		}
	}
}

func TestWithAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	account := domain.Account{ID: uuid.New()}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/talks", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithAccount(r, account)
		if found, ok := accountFromContext(r.Context()); !ok || found != account {
			t.Errorf("account not stored: %+v %v", found, ok)
		}
		w.WriteHeader(http.StatusCreated)
	})
	handler := withRequestID(withAccessLog(logger, mux))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/talks?secret=value", nil))
	requestID := recorder.Header().Get("X-Request-ID")
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if entry["request_id"] != requestID || entry["user_id"] != account.ID.String() || entry["status"] != float64(http.StatusCreated) {
		t.Fatalf("log entry %v", entry)
	}
	if entry["path"] != "/api/talks" {
		t.Fatalf("log path %v", entry["path"])
	}
	if strings.Contains(logs.String(), "secret") {
		t.Fatalf("log leaked query values: %s", logs.String())
	}
	if _, ok := entry["cookie"]; ok {
		t.Fatalf("log contains cookies: %s", logs.String())
	}
}

func TestWithAccessLogAnonymous(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/talks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := withAccessLog(logger, mux)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/talks", nil))
	if strings.Contains(logs.String(), "user_id") {
		t.Fatalf("anonymous request logged user id: %s", logs.String())
	}
}

func TestWithAccessLogRoutePatternOnly(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/people/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := withRequestID(withAccessLog(logger, mux))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/people/SECRET-MARKER?token=SECRET-MARKER", nil))
	if strings.Contains(logs.String(), "SECRET-MARKER") {
		t.Fatalf("log leaked path or query values: %s", logs.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if entry["path"] != "/api/people/{id}" {
		t.Fatalf("log path %v", entry["path"])
	}
	if entry["status"] != float64(http.StatusNoContent) || entry["request_id"] != recorder.Header().Get("X-Request-ID") {
		t.Fatalf("log entry %v", entry)
	}
}

func TestEndpointMethods(t *testing.T) {
	echo := func(method string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(method))
		})
	}
	handler := endpointMethods(map[string]http.Handler{
		http.MethodGet:    echo(http.MethodGet),
		http.MethodPost:   echo(http.MethodPost),
		http.MethodDelete: echo(http.MethodDelete),
	})
	cases := []struct {
		method string
		status int
	}{
		{http.MethodGet, http.StatusOK},
		{http.MethodHead, http.StatusOK},
		{http.MethodPost, http.StatusOK},
		{http.MethodPut, http.StatusMethodNotAllowed},
		{http.MethodPatch, http.StatusMethodNotAllowed},
	}
	for _, testCase := range cases {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(testCase.method, "/api/talks", nil))
		if recorder.Code != testCase.status {
			t.Fatalf("%s status %d, want %d", testCase.method, recorder.Code, testCase.status)
		}
		if testCase.status == http.StatusMethodNotAllowed {
			if allow := recorder.Header().Get("Allow"); allow != "DELETE, GET, HEAD, POST" {
				t.Fatalf("%s Allow %q", testCase.method, allow)
			}
			if code := errorCode(t, recorder); code != "method_not_allowed" {
				t.Fatalf("%s code %q", testCase.method, code)
			}
		}
	}
	postOnly := endpointMethods(map[string]http.Handler{http.MethodPost: echo(http.MethodPost)})
	recorder := httptest.NewRecorder()
	postOnly.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/api/talks", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "POST" {
		t.Fatalf("HEAD without GET: status %d Allow %q", recorder.Code, recorder.Header().Get("Allow"))
	}
}

func TestRequestAccountWithoutHolder(t *testing.T) {
	account := domain.Account{ID: uuid.New()}
	request := requestWithAccount(httptest.NewRequest(http.MethodGet, "/", nil), account)
	if found, ok := accountFromContext(request.Context()); !ok || found != account {
		t.Fatalf("account %+v %v", found, ok)
	}
	request = requestWithAccount(request, domain.Account{ID: account.ID, Role: "admin"})
	if found, _ := accountFromContext(request.Context()); found.Role != "admin" {
		t.Fatalf("account not replaced: %+v", found)
	}
	if _, ok := accountFromContext(context.Background()); ok {
		t.Fatal("background context has an account")
	}
}

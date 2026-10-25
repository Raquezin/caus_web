package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"caus-platform/api/internal/domain"
)

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error body %q: %v", recorder.Body.String(), err)
	}
	return response.Error.Code
}

func TestBackendErrorMappings(t *testing.T) {
	var logs bytes.Buffer
	backend := NewBackend(nil, nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"conflict", domain.ErrConflict, http.StatusConflict, "conflict"},
		{"pagination", domain.ErrInvalidPagination, http.StatusBadRequest, "invalid_pagination"},
		{"unknown", errors.New("sensitive database detail"), http.StatusInternalServerError, "internal_error"},
	}
	for _, testCase := range cases {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/talks/SECRET-MARKER?token=SECRET-MARKER", nil)
		request.Pattern = "/api/talks/{id}"
		backend.backendError(recorder, request, testCase.err)
		if recorder.Code != testCase.status {
			t.Fatalf("%s: status %d, want %d", testCase.name, recorder.Code, testCase.status)
		}
		if code := errorCode(t, recorder); code != testCase.code {
			t.Fatalf("%s: code %q, want %q", testCase.name, code, testCase.code)
		}
		if strings.Contains(recorder.Body.String(), "sensitive") {
			t.Fatalf("%s leaked raw error: %s", testCase.name, recorder.Body.String())
		}
	}
	if strings.Contains(logs.String(), "sensitive database detail") {
		t.Fatalf("logs leaked raw error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) || !strings.Contains(logs.String(), `"operation":"/api/talks/{id}"`) {
		t.Fatalf("logs missing operation or error type: %s", logs.String())
	}
	if strings.Contains(logs.String(), "SECRET-MARKER") {
		t.Fatalf("logs leaked path or query values: %s", logs.String())
	}
}

func TestBackendErrorLogsRoutePatternOnly(t *testing.T) {
	var logs bytes.Buffer
	backend := NewBackend(nil, nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/people/{id}", func(w http.ResponseWriter, r *http.Request) {
		backend.backendError(w, r, errors.New("SECRET-MARKER failure"))
	})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/people/SECRET-MARKER?token=SECRET-MARKER", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", recorder.Code)
	}
	if strings.Contains(logs.String(), "SECRET-MARKER") {
		t.Fatalf("logs leaked path, query or error values: %s", logs.String())
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &entry); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if entry["operation"] != "/api/people/{id}" {
		t.Fatalf("log operation %v", entry["operation"])
	}
	if _, ok := entry["path"]; ok {
		t.Fatalf("log contains raw path: %s", logs.String())
	}
}

func TestBackendErrorValidationFields(t *testing.T) {
	backend := NewBackend(nil, nil, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/talks", nil)
	backend.backendError(recorder, request, domain.ValidationError{Fields: map[string]string{"title": "title is required"}})
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", recorder.Code)
	}
	var response struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "validation_failed" || response.Error.Fields["title"] != "title is required" {
		t.Fatalf("response %+v", response.Error)
	}
}

func TestBackendErrorNil(t *testing.T) {
	backend := NewBackend(nil, nil, slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	recorder := httptest.NewRecorder()
	backend.backendError(recorder, httptest.NewRequest(http.MethodGet, "/", nil), nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("nil error wrote status %d", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("nil error wrote body %s", recorder.Body.String())
	}
}

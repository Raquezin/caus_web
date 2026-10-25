package httpapi

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTalkUpdateServiceErrors(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"conflict", domain.ErrConflict, http.StatusConflict, "conflict"},
		{"validation", domain.ValidationError{Fields: map[string]string{"speaker_ids": "invalid"}},
			http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &endpointTalkUpdateService{err: tc.err}
			auth := endpointTalkUpdateAuth(t, "admin")
			server := newEndpointTalkUpdateServer(t, auth, NewBackend(service, auth, logger))
			target := "/api/admin/talks/" + uuid.NewString()
			response, body := endpointTalkUpdateDo(t, server, http.MethodPatch, target, endpointTalkUpdateBody,
				endpointTalkUpdateCookies(), "https://example.test")
			if response.StatusCode != tc.status || endpointTalkUpdateErrorCode(t, body) != tc.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
			if tc.code == "validation_failed" && !bytes.Contains(body, []byte(`"speaker_ids"`)) {
				t.Fatalf("validation fields missing: %s", body)
			}
		})
	}
}

func TestEndpointTalkUpdateSanitizesFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &endpointTalkUpdateService{err: errors.New("pgx: password authentication failed")}
	auth := endpointTalkUpdateAuth(t, "admin")
	server := newEndpointTalkUpdateServer(t, auth, NewBackend(service, auth, logger))

	response, body := endpointTalkUpdateDo(t, server, http.MethodPatch, "/api/admin/talks/"+uuid.NewString(),
		endpointTalkUpdateBody, endpointTalkUpdateCookies(), "https://example.test")
	if response.StatusCode != http.StatusInternalServerError || endpointTalkUpdateErrorCode(t, body) != "internal_error" {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("password")) || bytes.Contains(logs.Bytes(), []byte("password")) {
		t.Fatalf("failure leaked detail: %s %s", body, logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
}

func TestEndpointTalkUpdateRejectsInvalidRequests(t *testing.T) {
	service := &endpointTalkUpdateService{talk: endpointTalkUpdateTalk()}
	auth := endpointTalkUpdateAuth(t, "admin")
	server := newEndpointTalkUpdateServer(t, auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	validTarget := "/api/admin/talks/" + service.talk.ID.String()

	cases := []struct {
		name   string
		target string
		body   string
		status int
		code   string
	}{
		{"malformed id", "/api/admin/talks/not-a-uuid", endpointTalkUpdateBody, http.StatusBadRequest, "invalid_id"},
		{"nil id", "/api/admin/talks/00000000-0000-0000-0000-000000000000", endpointTalkUpdateBody, http.StatusBadRequest, "invalid_id"},
		{"malformed json", validTarget, `{"title":`, http.StatusBadRequest, "invalid_body"},
		{"unknown field", validTarget, `{"unknown":true}`, http.StatusBadRequest, "invalid_body"},
		{"null title", validTarget, `{"title":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"null duration", validTarget, `{"duration":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"null speakers", validTarget, `{"speaker_ids":null}`, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, body := endpointTalkUpdateDo(t, server, http.MethodPatch, tc.target, tc.body,
				endpointTalkUpdateCookies(), "https://example.test")
			if response.StatusCode != tc.status || endpointTalkUpdateErrorCode(t, body) != tc.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
		})
	}
	if service.calls != 0 {
		t.Fatalf("invalid requests reached UpdateTalk %d times", service.calls)
	}
}

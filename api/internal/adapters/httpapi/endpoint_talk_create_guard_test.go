package httpapi

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTalkCreateGuards(t *testing.T) {
	speakerID := uuid.New()
	body := endpointTalkCreateJSON(t, endpointTalkCreateFields(speakerID))
	cases := []struct {
		name     string
		role     string
		signedIn bool
		origin   string
		status   int
		code     string
	}{
		{"member", "member", true, endpointTalkCreateOrigin, http.StatusForbidden, "forbidden"},
		{"anonymous", "admin", false, endpointTalkCreateOrigin, http.StatusUnauthorized, "unauthenticated"},
		{"missing origin", "admin", true, "", http.StatusForbidden, "origin_denied"},
		{"wrong origin", "admin", true, "https://evil.test", http.StatusForbidden, "origin_denied"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubEndpointTalkCreateService{}
			server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, testCase.role), endpointTalkCreateLogger())
			response, payload := endpointTalkCreateDo(t, server, "application/json", body, testCase.origin, testCase.signedIn)
			if response.StatusCode != testCase.status {
				t.Fatalf("status %d (%s)", response.StatusCode, payload)
			}
			if code, _ := endpointTalkCreateError(t, payload); code != testCase.code {
				t.Fatalf("code %q, want %q", code, testCase.code)
			}
			if service.calls != 0 {
				t.Fatalf("guarded request reached the service %d times", service.calls)
			}
		})
	}

	t.Run("duplicate origin", func(t *testing.T) {
		service := &stubEndpointTalkCreateService{}
		server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())
		request, err := http.NewRequest(http.MethodPost, server.URL+"/api/admin/talks", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Add("Origin", endpointTalkCreateOrigin)
		request.Header.Add("Origin", endpointTalkCreateOrigin)
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d (%s)", response.StatusCode, payload)
		}
		if code, _ := endpointTalkCreateError(t, payload); code != "origin_denied" {
			t.Fatalf("code %q", code)
		}
		if service.calls != 0 {
			t.Fatalf("duplicate origin reached the service %d times", service.calls)
		}
	})
}

func TestEndpointTalkCreateConflictAndFailure(t *testing.T) {
	speakerID := uuid.New()
	body := endpointTalkCreateJSON(t, endpointTalkCreateFields(speakerID))

	t.Run("conflict", func(t *testing.T) {
		service := &stubEndpointTalkCreateService{err: domain.ErrConflict}
		server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())
		response, payload := endpointTalkCreateDo(t, server, "application/json", body, endpointTalkCreateOrigin, true)
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("status %d (%s)", response.StatusCode, payload)
		}
		if code, _ := endpointTalkCreateError(t, payload); code != "conflict" {
			t.Fatalf("code %q", code)
		}
	})

	t.Run("unregistered speaker", func(t *testing.T) {
		service := &stubEndpointTalkCreateService{err: domain.ValidationError{
			Fields: map[string]string{"speaker_ids": "speaker_ids must reference registered people"},
		}}
		server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())
		response, payload := endpointTalkCreateDo(t, server, "application/json", body, endpointTalkCreateOrigin, true)
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status %d (%s)", response.StatusCode, payload)
		}
		code, fields := endpointTalkCreateError(t, payload)
		if code != "validation_failed" || fields["speaker_ids"] == "" {
			t.Fatalf("code %q fields %v", code, fields)
		}
	})

	t.Run("sanitized internal error", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		service := &stubEndpointTalkCreateService{err: errors.New("insert failed: password=SECRET-MARKER")}
		server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), logger)
		response, payload := endpointTalkCreateDo(t, server, "application/json", body, endpointTalkCreateOrigin, true)
		if response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status %d (%s)", response.StatusCode, payload)
		}
		if code, _ := endpointTalkCreateError(t, payload); code != "internal_error" {
			t.Fatalf("code %q", code)
		}
		if bytes.Contains(payload, []byte("SECRET-MARKER")) || bytes.Contains(payload, []byte("password")) {
			t.Fatalf("response leaked detail: %s", payload)
		}
		if bytes.Contains(logs.Bytes(), []byte("SECRET-MARKER")) || bytes.Contains(logs.Bytes(), []byte("password")) {
			t.Fatalf("logs leaked detail: %s", logs.String())
		}
		if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
			t.Fatalf("logs missing error type: %s", logs.String())
		}
	})
}

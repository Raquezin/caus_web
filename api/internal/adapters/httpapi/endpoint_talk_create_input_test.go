package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/application"
)

func TestEndpointTalkCreateRejectsBadBodies(t *testing.T) {
	speakerID := uuid.New()
	valid := endpointTalkCreateJSON(t, endpointTalkCreateFields(speakerID))
	cases := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{"unknown field", "application/json", `{"title":"Talk","unknown_field":true}`, http.StatusBadRequest, "invalid_body"},
		{"malformed json", "application/json", `{"title":`, http.StatusBadRequest, "invalid_body"},
		{"null body", "application/json", `null`, http.StatusBadRequest, "invalid_body"},
		{"empty body", "application/json", `   `, http.StatusBadRequest, "invalid_body"},
		{"trailing value", "application/json", valid + `{}`, http.StatusBadRequest, "invalid_body"},
		{"array body", "application/json", `[]`, http.StatusBadRequest, "invalid_body"},
		{"wrong content type", "text/plain", valid, http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"missing content type", "", valid, http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"oversized body", "application/json", `{"title":"` + strings.Repeat("a", maxJSONBody) + `"}`,
			http.StatusRequestEntityTooLarge, "request_too_large"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubEndpointTalkCreateService{}
			server := endpointTalkCreateServer(t, service, endpointTalkCreateAuth(t, "admin"), endpointTalkCreateLogger())
			response, payload := endpointTalkCreateDo(t, server, testCase.contentType, testCase.body,
				endpointTalkCreateOrigin, true)
			if response.StatusCode != testCase.status {
				t.Fatalf("status %d, want %d (%s)", response.StatusCode, testCase.status, payload)
			}
			if code, _ := endpointTalkCreateError(t, payload); code != testCase.code {
				t.Fatalf("code %q, want %q", code, testCase.code)
			}
			if service.calls != 0 {
				t.Fatalf("rejected body reached the service %d times", service.calls)
			}
		})
	}
}

func TestEndpointTalkCreateValidatesInput(t *testing.T) {
	speakerID := uuid.MustParse("3b6f0b6e-4c1a-4f7e-9a3e-1d2c3b4a5f60")
	cases := []struct {
		name   string
		field  string
		mutate func(map[string]any)
	}{
		{"missing title", "title", func(fields map[string]any) { delete(fields, "title") }},
		{"null title", "title", func(fields map[string]any) { fields["title"] = nil }},
		{"blank description", "description", func(fields map[string]any) { fields["description"] = "   " }},
		{"null description", "description", func(fields map[string]any) { fields["description"] = nil }},
		{"blank location", "location", func(fields map[string]any) { fields["location"] = "" }},
		{"unknown language", "language", func(fields map[string]any) { fields["language"] = "fr" }},
		{"null language", "language", func(fields map[string]any) { fields["language"] = nil }},
		{"offset date", "date", func(fields map[string]any) { fields["date"] = "2026-05-01T18:00:00+02:00" }},
		{"invalid date", "date", func(fields map[string]any) { fields["date"] = "2026-02-30T18:00:00" }},
		{"null date", "date", func(fields map[string]any) { fields["date"] = nil }},
		{"zero duration", "duration", func(fields map[string]any) { fields["duration"] = 0 }},
		{"negative duration", "duration", func(fields map[string]any) { fields["duration"] = -30 }},
		{"null duration", "duration", func(fields map[string]any) { fields["duration"] = nil }},
		{"invalid stream url", "stream_url", func(fields map[string]any) { fields["stream_url"] = "javascript:alert(1)" }},
		{"invalid poster url", "poster_url", func(fields map[string]any) { fields["poster_url"] = "example.com/poster" }},
		{"empty speakers", "speaker_ids", func(fields map[string]any) { fields["speaker_ids"] = []uuid.UUID{} }},
		{"null speakers", "speaker_ids", func(fields map[string]any) { fields["speaker_ids"] = nil }},
		{"duplicate speakers", "speaker_ids", func(fields map[string]any) {
			fields["speaker_ids"] = []uuid.UUID{speakerID, speakerID}
		}},
		{"nil speaker", "speaker_ids", func(fields map[string]any) { fields["speaker_ids"] = []uuid.UUID{uuid.Nil} }},
		{"malformed speaker id", "speaker_ids", func(fields map[string]any) { fields["speaker_ids"] = []string{"nope"} }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &stubEndpointTalkCreateRepo{}
			server := endpointTalkCreateServer(t, application.NewBackend(repo), endpointTalkCreateAuth(t, "admin"),
				endpointTalkCreateLogger())
			fields := endpointTalkCreateFields(speakerID)
			testCase.mutate(fields)
			response, payload := endpointTalkCreateDo(t, server, "application/json",
				endpointTalkCreateJSON(t, fields), endpointTalkCreateOrigin, true)
			if response.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422 (%s)", response.StatusCode, payload)
			}
			code, errorFields := endpointTalkCreateError(t, payload)
			if code != "validation_failed" {
				t.Fatalf("code %q", code)
			}
			if errorFields[testCase.field] == "" {
				t.Fatalf("fields %v missing %q", errorFields, testCase.field)
			}
			if repo.calls != 0 {
				t.Fatalf("invalid input reached the repository %d times", repo.calls)
			}
		})
	}
}

package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"caus-platform/api/internal/domain"
)

func endpointCompetitionUpdateErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return response.Error.Code
}

func endpointCompetitionUpdateFields(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var response struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode validation body %q: %v", body, err)
	}
	return response.Error.Fields
}

func TestEndpointCompetitionUpdateRejectsInvalidRequest(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	validTarget := endpointCompetitionUpdateTarget(repo.competition.ID)

	cases := []struct {
		name        string
		target      string
		contentType string
		body        string
		status      int
		code        string
		field       string
	}{
		{"invalid id", "/api/admin/competitions/not-a-uuid", "application/json", `{"name":"Autumn"}`,
			http.StatusBadRequest, "invalid_id", ""},
		{"malformed", validTarget, "application/json", `{"name":`, http.StatusBadRequest, "invalid_body", ""},
		{"unknown field", validTarget, "application/json", `{"name":"Autumn","published":true}`,
			http.StatusBadRequest, "invalid_body", ""},
		{"wrong content type", validTarget, "text/plain", `{"name":"Autumn"}`,
			http.StatusUnsupportedMediaType, "unsupported_media_type", ""},
		{"null name", validTarget, "application/json", `{"name":null}`,
			http.StatusUnprocessableEntity, "validation_failed", "name"},
		{"null date", validTarget, "application/json", `{"date":null}`,
			http.StatusUnprocessableEntity, "validation_failed", "date"},
		{"null team size", validTarget, "application/json", `{"team_size":null}`,
			http.StatusUnprocessableEntity, "validation_failed", "team_size"},
		{"empty patch", validTarget, "application/json", `{}`,
			http.StatusUnprocessableEntity, "validation_failed", "patch"},
		{"blank name", validTarget, "application/json", `{"name":"  "}`,
			http.StatusUnprocessableEntity, "validation_failed", "name"},
		{"zero team size", validTarget, "application/json", `{"team_size":0}`,
			http.StatusUnprocessableEntity, "validation_failed", "team_size"},
		{"negative team size", validTarget, "application/json", `{"team_size":-1}`,
			http.StatusUnprocessableEntity, "validation_failed", "team_size"},
		{"large team size", validTarget, "application/json", `{"team_size":32768}`,
			http.StatusUnprocessableEntity, "validation_failed", "team_size"},
		{"invalid date", validTarget, "application/json", `{"date":"2026-02-30T19:30:00"}`,
			http.StatusUnprocessableEntity, "validation_failed", "date"},
		{"offset date", validTarget, "application/json", `{"date":"2026-07-18T19:30:00+02:00"}`,
			http.StatusUnprocessableEntity, "validation_failed", "date"},
		{"nonexistent local date", validTarget, "application/json", `{"date":"2026-03-29T02:30:00"}`,
			http.StatusUnprocessableEntity, "validation_failed", "date"},
	}
	for _, testCase := range cases {
		response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch, testCase.target,
			"https://example.test", testCase.contentType, testCase.body, endpointCompetitionUpdateSession())
		if response.StatusCode != testCase.status {
			t.Fatalf("%s: status %d (%s)", testCase.name, response.StatusCode, body)
		}
		if code := endpointCompetitionUpdateErrorCode(t, body); code != testCase.code {
			t.Fatalf("%s: code %q", testCase.name, code)
		}
		if testCase.field != "" {
			if fields := endpointCompetitionUpdateFields(t, body); fields[testCase.field] == "" {
				t.Fatalf("%s: fields %v", testCase.name, fields)
			}
		}
	}
	if repo.calls != 0 {
		t.Fatalf("invalid requests reached the service %d times", repo.calls)
	}
}

func TestEndpointCompetitionUpdateRejectsOversizedBody(t *testing.T) {
	repo := &endpointCompetitionUpdateRepo{competition: endpointCompetitionUpdateFixture()}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
		slog.New(slog.NewJSONHandler(io.Discard, nil)))
	body := `{"name":"` + strings.Repeat("a", maxJSONBody) + `"}`

	response, data := endpointCompetitionUpdateDo(t, server, http.MethodPatch,
		endpointCompetitionUpdateTarget(repo.competition.ID), "https://example.test", "application/json", body,
		endpointCompetitionUpdateSession())
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", response.StatusCode)
	}
	if code := endpointCompetitionUpdateErrorCode(t, data); code != "request_too_large" {
		t.Fatalf("code %q", code)
	}
	if repo.calls != 0 {
		t.Fatalf("oversized request reached the service %d times", repo.calls)
	}
}

func TestEndpointCompetitionUpdateMapsRepositoryErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		field  string
	}{
		{"missing", domain.ErrNotFound, http.StatusNotFound, "not_found", ""},
		{"duplicate", domain.ErrConflict, http.StatusConflict, "conflict", ""},
		{"shrink", domain.ValidationError{Fields: map[string]string{"data": "invalid value"}},
			http.StatusUnprocessableEntity, "validation_failed", "data"},
	}
	for _, testCase := range cases {
		repo := &endpointCompetitionUpdateRepo{err: testCase.err}
		server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo,
			slog.New(slog.NewJSONHandler(io.Discard, nil)))
		response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch,
			endpointCompetitionUpdateTarget(endpointCompetitionUpdateFixture().ID), "https://example.test",
			"application/json", `{"team_size":2}`, endpointCompetitionUpdateSession())
		if response.StatusCode != testCase.status {
			t.Fatalf("%s: status %d (%s)", testCase.name, response.StatusCode, body)
		}
		if code := endpointCompetitionUpdateErrorCode(t, body); code != testCase.code {
			t.Fatalf("%s: code %q", testCase.name, code)
		}
		if testCase.field != "" {
			if fields := endpointCompetitionUpdateFields(t, body); fields[testCase.field] == "" {
				t.Fatalf("%s: fields %v", testCase.name, fields)
			}
		}
		if repo.calls != 1 {
			t.Fatalf("%s: service calls %d", testCase.name, repo.calls)
		}
	}
}

package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestEndpointCompetitionUpdateSanitizesRepositoryFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	repo := &endpointCompetitionUpdateRepo{err: errors.New("password rejected for database")}
	server := newEndpointCompetitionUpdateServer(t, endpointCompetitionUpdateAuth(t, "admin"), repo, logger)

	response, body := endpointCompetitionUpdateDo(t, server, http.MethodPatch,
		endpointCompetitionUpdateTarget(endpointCompetitionUpdateFixture().ID), "https://example.test",
		"application/json", `{"name":"Autumn"}`, endpointCompetitionUpdateSession())
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if code := endpointCompetitionUpdateErrorCode(t, body); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	if strings.Contains(string(body), "password") {
		t.Fatalf("response leaked repository detail: %s", body)
	}
	if repo.calls != 1 {
		t.Fatalf("service calls %d", repo.calls)
	}
	if strings.Contains(logs.String(), "password") {
		t.Fatalf("logs leaked repository detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"error_type"`) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"operation":"/api/admin/competitions/{id}"`) {
		t.Fatalf("logs missing route pattern: %s", logs.String())
	}
}

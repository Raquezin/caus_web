package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"caus-platform/api/internal/domain"
)

func TestEndpointTeamUpdateMapsServiceErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found", ""},
		{"conflict", domain.ErrConflict, http.StatusConflict, "conflict", ""},
		{"validation", domain.ValidationError{Fields: map[string]string{"position": "position must be between 1 and 32767"}},
			http.StatusUnprocessableEntity, "validation_failed", ""},
		{"internal", errors.New("database password rejected"), http.StatusInternalServerError, "internal_error", "password"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var logs bytes.Buffer
			auth, service := endpointTeamUpdateAdmin(t)
			service.team = endpointTeamUpdateTeam()
			service.err = testCase.err
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			server := newEndpointTeamUpdateServer(t, auth, NewBackend(service, auth, logger))
			response, body := endpointTeamUpdateDo(t, server, http.MethodPatch,
				"/api/admin/teams/"+service.team.ID.String(), `{"name":"Team B"}`,
				[]*http.Cookie{endpointTeamUpdateSession()}, "https://example.test")
			if response.StatusCode != testCase.status || endpointTeamUpdateErrorCode(t, body) != testCase.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
			if testCase.message != "" && (bytes.Contains(body, []byte(testCase.message)) ||
				bytes.Contains(logs.Bytes(), []byte(testCase.message))) {
				t.Fatalf("failure leaked detail: body=%s logs=%s", body, logs.String())
			}
		})
	}
}

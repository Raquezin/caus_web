package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTeamCreateGuards(t *testing.T) {
	service := &endpointTeamCreateService{team: endpointTeamCreateTeam()}
	competitionID := uuid.New()
	body := `{"member_ids":["` + uuid.NewString() + `"]}`

	cases := []struct {
		name    string
		account domain.Account
		authErr error
		cookies []*http.Cookie
		origins []string
		status  int
		code    string
	}{
		{"missing session", domain.Account{ID: uuid.New(), Role: "admin"}, nil, nil,
			[]string{"https://example.test"}, http.StatusUnauthorized, "unauthenticated"},
		{"expired session", domain.Account{}, domain.ErrUnauthenticated, endpointTeamCreateSession(),
			[]string{"https://example.test"}, http.StatusUnauthorized, "unauthenticated"},
		{"member role", domain.Account{ID: uuid.New(), Role: "member"}, nil, endpointTeamCreateSession(),
			[]string{"https://example.test"}, http.StatusForbidden, "forbidden"},
		{"missing origin", domain.Account{ID: uuid.New(), Role: "admin"}, nil, endpointTeamCreateSession(),
			nil, http.StatusForbidden, "origin_denied"},
		{"wrong origin", domain.Account{ID: uuid.New(), Role: "admin"}, nil, endpointTeamCreateSession(),
			[]string{"https://evil.test"}, http.StatusForbidden, "origin_denied"},
		{"duplicate origin", domain.Account{ID: uuid.New(), Role: "admin"}, nil, endpointTeamCreateSession(),
			[]string{"https://example.test", "https://example.test"}, http.StatusForbidden, "origin_denied"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			auth := endpointTeamCreateAuth(t, testCase.account, testCase.authErr)
			handler := endpointTeamCreateHandler(service, auth, nil)
			recorder := endpointTeamCreateDo(handler, competitionID.String(), body, "application/json",
				testCase.cookies, testCase.origins)
			assertBackendError(t, recorder, testCase.status, testCase.code)
			if service.calls != 0 {
				t.Fatalf("guarded request reached CreateTeam %d times", service.calls)
			}
		})
	}
}

func TestEndpointTeamCreateInvalidCompetitionID(t *testing.T) {
	service := &endpointTeamCreateService{team: endpointTeamCreateTeam()}
	auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	handler := endpointTeamCreateHandler(service, auth, nil)
	body := `{"member_ids":["` + uuid.NewString() + `"]}`

	for _, id := range []string{"not-a-uuid", uuid.Nil.String()} {
		recorder := endpointTeamCreateDo(handler, id, body, "application/json",
			endpointTeamCreateSession(), []string{"https://example.test"})
		assertBackendError(t, recorder, http.StatusBadRequest, "invalid_id")
	}
	if service.calls != 0 {
		t.Fatalf("invalid ids reached CreateTeam %d times", service.calls)
	}
}

func TestEndpointTeamCreatePayload(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        string
	}{
		{"malformed json", "application/json", `{`, http.StatusBadRequest, "invalid_body"},
		{"unknown field", "application/json", `{"member_ids":[],"id":"x"}`, http.StatusBadRequest, "invalid_body"},
		{"multiple values", "application/json", `{"member_ids":["` + uuid.NewString() + `"]}{"member_ids":["` +
			uuid.NewString() + `"]}`, http.StatusBadRequest, "invalid_body"},
		{"empty body", "application/json", ``, http.StatusBadRequest, "invalid_body"},
		{"null body", "application/json", `null`, http.StatusBadRequest, "invalid_body"},
		{"wrong content type", "text/plain", `{"member_ids":["` + uuid.NewString() + `"]}`,
			http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"oversized body", "application/json", strings.Repeat("x", 70*1024),
			http.StatusRequestEntityTooLarge, "request_too_large"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &endpointTeamCreateService{team: endpointTeamCreateTeam()}
			auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
			handler := endpointTeamCreateHandler(service, auth, nil)
			recorder := endpointTeamCreateDo(handler, uuid.NewString(), testCase.body, testCase.contentType,
				endpointTeamCreateSession(), []string{"https://example.test"})
			assertBackendError(t, recorder, testCase.status, testCase.code)
			if service.calls != 0 {
				t.Fatalf("invalid payload reached CreateTeam %d times", service.calls)
			}
		})
	}
}

func TestEndpointTeamCreateDomainErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", domain.ValidationError{Fields: map[string]string{"member_ids": "required"}},
			http.StatusUnprocessableEntity, "validation_failed"},
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"conflict", domain.ErrConflict, http.StatusConflict, "conflict"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &endpointTeamCreateService{err: testCase.err}
			auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
			handler := endpointTeamCreateHandler(service, auth, nil)
			recorder := endpointTeamCreateDo(handler, uuid.NewString(),
				`{"member_ids":["`+uuid.NewString()+`"]}`, "application/json",
				endpointTeamCreateSession(), []string{"https://example.test"})
			assertBackendError(t, recorder, testCase.status, testCase.code)
			if service.calls != 1 {
				t.Fatalf("calls %d", service.calls)
			}
			if testCase.status == http.StatusUnprocessableEntity {
				var response struct {
					Error struct {
						Fields map[string]string `json:"fields"`
					} `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
				}
				if response.Error.Fields["member_ids"] == "" {
					t.Fatalf("fields %v missing member_ids", response.Error.Fields)
				}
			}
		})
	}
}

func TestEndpointTeamCreateSanitizesFailure(t *testing.T) {
	var logs bytes.Buffer
	service := &endpointTeamCreateService{err: errors.New("password authentication failed for user caus")}
	auth := endpointTeamCreateAuth(t, domain.Account{ID: uuid.New(), Role: "admin"}, nil)
	handler := endpointTeamCreateHandler(service, auth, &logs)

	recorder := endpointTeamCreateDo(handler, uuid.NewString(),
		`{"member_ids":["`+uuid.NewString()+`"]}`, "application/json",
		endpointTeamCreateSession(), []string{"https://example.test"})
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("body leaked failure detail: %s", recorder.Body.String())
	}
	if strings.Contains(logs.String(), "password") {
		t.Fatalf("logs leaked failure detail: %s", logs.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if entry["error_type"] != "*errors.errorString" ||
		entry["operation"] != "POST /api/admin/competitions/{id}/teams" {
		t.Fatalf("log entry %v", entry)
	}
	if service.calls != 1 {
		t.Fatalf("calls %d", service.calls)
	}
}

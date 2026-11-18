package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointAccountUpdateService struct {
	BackendService
	account domain.Account
	err     error

	calls       int
	id          uuid.UUID
	patch       domain.AccountPatch
	hasDeadline bool
	deadline    time.Duration
}

func (s *endpointAccountUpdateService) UpdateAccount(ctx context.Context, id uuid.UUID,
	patch domain.AccountPatch) (domain.Account, error) {
	s.calls++
	s.id = id
	s.patch = patch
	if deadline, ok := ctx.Deadline(); ok {
		s.hasDeadline = true
		s.deadline = time.Until(deadline)
	}
	if s.err != nil {
		return domain.Account{}, s.err
	}
	updated := s.account
	if patch.FirstName != nil {
		updated.FirstName = *patch.FirstName
	}
	if patch.LastName != nil {
		updated.LastName = *patch.LastName
	}
	return updated, nil
}

type endpointAccountUpdateFixture struct {
	service *endpointAccountUpdateService
	server  *httptest.Server
	account domain.Account
}

func newEndpointAccountUpdateFixture(t *testing.T) *endpointAccountUpdateFixture {
	t.Helper()
	account := endpointMeAccount()
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	service := &endpointAccountUpdateService{account: account}
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.Handle("/api/me", auth.requireUser(auth.requireSameOrigin(endpointMethods(map[string]http.Handler{
		http.MethodPatch: http.HandlerFunc(backend.updateAccount),
	}))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &endpointAccountUpdateFixture{service: service, server: server, account: account}
}

func (f *endpointAccountUpdateFixture) do(t *testing.T, body, contentType, origin string,
	session bool) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPatch, f.server.URL+"/api/me", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if session {
		request.AddCookie(endpointMeSessionCookie())
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, payload
}

func (f *endpointAccountUpdateFixture) patch(t *testing.T, body string) (*http.Response, []byte) {
	t.Helper()
	return f.do(t, body, "application/json", "https://example.test", true)
}

func endpointAccountUpdateErrorCode(t *testing.T, body []byte) string {
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

func TestEndpointAccountUpdateRequiresSessionAndOrigin(t *testing.T) {
	fixture := newEndpointAccountUpdateFixture(t)

	anonymous, body := fixture.do(t, `{"first_name":"Grace"}`, "application/json", "https://example.test", false)
	if anonymous.StatusCode != http.StatusUnauthorized || endpointAccountUpdateErrorCode(t, body) != "unauthenticated" {
		t.Fatalf("missing session: %d (%s)", anonymous.StatusCode, body)
	}

	for name, origin := range map[string]string{"missing": "", "wrong": "https://attacker.test"} {
		response, body := fixture.do(t, `{"first_name":"Grace"}`, "application/json", origin, true)
		if response.StatusCode != http.StatusForbidden || endpointAccountUpdateErrorCode(t, body) != "origin_denied" {
			t.Fatalf("%s origin: %d (%s)", name, response.StatusCode, body)
		}
	}
	if fixture.service.calls != 0 {
		t.Fatalf("guarded requests reached UpdateAccount %d times", fixture.service.calls)
	}
}

func TestEndpointAccountUpdateRejectsInvalidRequests(t *testing.T) {
	fixture := newEndpointAccountUpdateFixture(t)
	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"unknown field", `{"nickname":"Grace"}`, http.StatusBadRequest, "invalid_body"},
		{"malformed", `{`, http.StatusBadRequest, "invalid_body"},
		{"null first name", `{"first_name":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"number first name", `{"first_name":7}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"null last name", `{"last_name":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"number last name", `{"last_name":7}`, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response, body := fixture.patch(t, testCase.body)
			if response.StatusCode != testCase.status || endpointAccountUpdateErrorCode(t, body) != testCase.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
		})
	}

	unsupported, body := fixture.do(t, `{"first_name":"Grace"}`, "text/plain", "https://example.test", true)
	if unsupported.StatusCode != http.StatusUnsupportedMediaType ||
		endpointAccountUpdateErrorCode(t, body) != "unsupported_media_type" {
		t.Fatalf("non-JSON content type: %d (%s)", unsupported.StatusCode, body)
	}
	if fixture.service.calls != 0 {
		t.Fatalf("invalid requests reached UpdateAccount %d times", fixture.service.calls)
	}
}

func TestEndpointAccountUpdateMapsServiceErrors(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"validation", domain.ValidationError{Fields: map[string]string{"last_name": "last_name contains invalid text"}},
			http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newEndpointAccountUpdateFixture(t)
			fixture.service.err = testCase.err
			response, body := fixture.patch(t, `{"first_name":"Grace"}`)
			if response.StatusCode != testCase.status || endpointAccountUpdateErrorCode(t, body) != testCase.code {
				t.Fatalf("status %d (%s)", response.StatusCode, body)
			}
		})
	}
}

func TestEndpointAccountUpdateReturnsUpdatedAccount(t *testing.T) {
	fixture := newEndpointAccountUpdateFixture(t)
	response, body := fixture.patch(t, `{"first_name":"  Grace ","last_name":"   "}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode account body %q: %v", body, err)
	}
	publicFields := map[string]bool{
		"id": true, "first_name": true, "last_name": true,
		"email": true, "role": true, "created_at": true, "updated_at": true,
	}
	if len(payload) != len(publicFields) {
		t.Fatalf("account exposes %d fields: %v", len(payload), payload)
	}
	if payload["id"] != fixture.account.ID.String() || payload["first_name"] != "Grace" || payload["last_name"] != "" ||
		payload["email"] != fixture.account.Email {
		t.Fatalf("updated account payload %v", payload)
	}

	if fixture.service.calls != 1 || fixture.service.id != fixture.account.ID {
		t.Fatalf("UpdateAccount calls=%d id=%s", fixture.service.calls, fixture.service.id)
	}
	patch := fixture.service.patch
	if patch.FirstName == nil || *patch.FirstName != "Grace" ||
		patch.LastName == nil || *patch.LastName != "" {
		t.Fatalf("forwarded patch %+v", patch)
	}
	if !fixture.service.hasDeadline || fixture.service.deadline <= 0 || fixture.service.deadline > 5*time.Second {
		t.Fatalf("service deadline %v (present %v)", fixture.service.deadline, fixture.service.hasDeadline)
	}
}

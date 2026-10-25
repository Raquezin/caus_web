package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const endpointCompetitionDeleteOrigin = "https://example.test"

type EndpointCompetitionDeleteService struct {
	BackendService
	id        uuid.UUID
	remaining time.Duration
	hasBound  bool
	calls     int
	err       error
}

func (s *EndpointCompetitionDeleteService) DeleteCompetition(ctx context.Context, id uuid.UUID) error {
	s.calls++
	s.id = id
	deadline, ok := ctx.Deadline()
	s.hasBound = ok
	if ok {
		s.remaining = time.Until(deadline)
	}
	return s.err
}

func endpointCompetitionDeleteAccount(role string) domain.Account {
	return domain.Account{
		ID:        uuid.MustParse("0b7c9bda-4f2e-4a1c-9a67-2f3f5c8d1e44"),
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Role:      role,
	}
}

func endpointCompetitionDeleteServer(t *testing.T, account domain.Account, service BackendService, logger *slog.Logger) *httptest.Server {
	t.Helper()
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, endpointCompetitionDeleteOrigin)
	backend := NewBackend(service, auth, logger)
	mux := http.NewServeMux()
	mux.Handle("/api/admin/competitions/{id}", auth.requireAdmin(auth.requireSameOrigin(endpointMethods(
		map[string]http.Handler{http.MethodDelete: http.HandlerFunc(backend.deleteCompetition)}))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func endpointCompetitionDeleteCall(t *testing.T, server *httptest.Server, method, id string, session bool,
	mutate func(*http.Request)) (*http.Response, []byte) {
	t.Helper()
	target := server.URL + "/api/admin/competitions/" + id
	request, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if session {
		request.AddCookie(&http.Cookie{Name: "__Host-caus_session", Value: "session-token"})
	}
	if mutate != nil {
		mutate(request)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

func endpointCompetitionDeleteSendOrigin(request *http.Request) {
	request.Header.Set("Origin", endpointCompetitionDeleteOrigin)
}

func endpointCompetitionDeleteCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return payload.Error.Code
}

func TestEndpointCompetitionDeleteSuccess(t *testing.T) {
	service := &EndpointCompetitionDeleteService{}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service, nil)
	id := uuid.New()

	response, body := endpointCompetitionDeleteCall(t, server, http.MethodDelete, id.String(), true,
		endpointCompetitionDeleteSendOrigin)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if len(body) != 0 {
		t.Fatalf("204 returned a body: %s", body)
	}
	if service.calls != 1 || service.id != id {
		t.Fatalf("delete calls %d for id %s, want 1 for %s", service.calls, service.id, id)
	}
	if !service.hasBound || service.remaining <= 0 || service.remaining > lookupTimeout {
		t.Fatalf("handler context deadline %v (bound %v), want within %v", service.remaining, service.hasBound, lookupTimeout)
	}
}

func TestEndpointCompetitionDeleteMissingAndRepeat(t *testing.T) {
	service := &EndpointCompetitionDeleteService{err: domain.ErrNotFound}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service, nil)
	id := uuid.New().String()

	for attempt := 1; attempt <= 2; attempt++ {
		response, body := endpointCompetitionDeleteCall(t, server, http.MethodDelete, id, true,
			endpointCompetitionDeleteSendOrigin)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("attempt %d status %d (%s)", attempt, response.StatusCode, body)
		}
		if code := endpointCompetitionDeleteCode(t, body); code != "not_found" {
			t.Fatalf("attempt %d code %q", attempt, code)
		}
	}
	if service.calls != 2 {
		t.Fatalf("delete calls %d, want 2", service.calls)
	}
}

func TestEndpointCompetitionDeleteMalformedID(t *testing.T) {
	service := &EndpointCompetitionDeleteService{}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service, nil)

	for _, id := range []string{"not-a-uuid", uuid.Nil.String()} {
		response, body := endpointCompetitionDeleteCall(t, server, http.MethodDelete, id, true,
			endpointCompetitionDeleteSendOrigin)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("id %q status %d (%s)", id, response.StatusCode, body)
		}
		if code := endpointCompetitionDeleteCode(t, body); code != "invalid_id" {
			t.Fatalf("id %q code %q", id, code)
		}
	}
	if service.calls != 0 {
		t.Fatalf("malformed id reached the service %d times", service.calls)
	}
}

func TestEndpointCompetitionDeleteRequiresAdmin(t *testing.T) {
	memberService := &EndpointCompetitionDeleteService{}
	memberServer := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("member"), memberService, nil)
	response, body := endpointCompetitionDeleteCall(t, memberServer, http.MethodDelete, uuid.NewString(), true,
		endpointCompetitionDeleteSendOrigin)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("member status %d (%s)", response.StatusCode, body)
	}
	if code := endpointCompetitionDeleteCode(t, body); code != "forbidden" {
		t.Fatalf("member code %q", code)
	}
	if memberService.calls != 0 {
		t.Fatalf("member reached the service %d times", memberService.calls)
	}

	anonymousService := &EndpointCompetitionDeleteService{}
	anonymousServer := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), anonymousService, nil)
	anonymous, body := endpointCompetitionDeleteCall(t, anonymousServer, http.MethodDelete, uuid.NewString(), false,
		endpointCompetitionDeleteSendOrigin)
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status %d (%s)", anonymous.StatusCode, body)
	}
	if code := endpointCompetitionDeleteCode(t, body); code != "unauthenticated" {
		t.Fatalf("anonymous code %q", code)
	}
	if anonymousService.calls != 0 {
		t.Fatalf("anonymous request reached the service %d times", anonymousService.calls)
	}
}

func TestEndpointCompetitionDeleteRequiresOrigin(t *testing.T) {
	service := &EndpointCompetitionDeleteService{}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service, nil)

	cases := map[string]func(*http.Request){
		"missing": nil,
		"wrong": func(request *http.Request) {
			request.Header.Set("Origin", "https://intruder.test")
		},
		"duplicate": func(request *http.Request) {
			request.Header.Add("Origin", endpointCompetitionDeleteOrigin)
			request.Header.Add("Origin", endpointCompetitionDeleteOrigin)
		},
	}
	for name, mutate := range cases {
		response, body := endpointCompetitionDeleteCall(t, server, http.MethodDelete, uuid.NewString(), true, mutate)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s origin status %d (%s)", name, response.StatusCode, body)
		}
		if code := endpointCompetitionDeleteCode(t, body); code != "origin_denied" {
			t.Fatalf("%s origin code %q", name, code)
		}
	}
	if service.calls != 0 {
		t.Fatalf("origin checks reached the service %d times", service.calls)
	}
}

func TestEndpointCompetitionDeleteBackendFailure(t *testing.T) {
	var logs bytes.Buffer
	service := &EndpointCompetitionDeleteService{err: errors.New("password authentication failed for user caus")}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service,
		slog.New(slog.NewJSONHandler(&logs, nil)))

	response, body := endpointCompetitionDeleteCall(t, server, http.MethodDelete, uuid.NewString(), true,
		endpointCompetitionDeleteSendOrigin)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if code := endpointCompetitionDeleteCode(t, body); code != "internal_error" {
		t.Fatalf("code %q", code)
	}
	if bytes.Contains(body, []byte("password")) {
		t.Fatalf("failure body leaked detail: %s", body)
	}
	if bytes.Contains(logs.Bytes(), []byte("password")) {
		t.Fatalf("logs leaked detail: %s", logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
	if service.calls != 1 {
		t.Fatalf("delete calls %d, want 1", service.calls)
	}
}

func TestEndpointCompetitionDeleteWrongMethod(t *testing.T) {
	service := &EndpointCompetitionDeleteService{}
	server := endpointCompetitionDeleteServer(t, endpointCompetitionDeleteAccount("admin"), service, nil)

	response, body := endpointCompetitionDeleteCall(t, server, http.MethodPost, uuid.NewString(), true,
		endpointCompetitionDeleteSendOrigin)
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if allow := response.Header.Get("Allow"); allow != http.MethodDelete {
		t.Fatalf("Allow %q", allow)
	}
	if code := endpointCompetitionDeleteCode(t, body); code != "method_not_allowed" {
		t.Fatalf("code %q", code)
	}
	if service.calls != 0 {
		t.Fatalf("wrong method reached the service %d times", service.calls)
	}
}

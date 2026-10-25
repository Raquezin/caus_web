package httpapi

import (
	"bytes"
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

func newEndpointMeServer(t *testing.T, auth *Auth, backend *Backend) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(auth.requireUser(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.me),
	})))
	t.Cleanup(server.Close)
	return server
}

func endpointMeDo(t *testing.T, server *httptest.Server, method string, cookies []*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return response, body
}

func endpointMeSessionCookie() *http.Cookie {
	return &http.Cookie{Name: "__Host-caus_session", Value: "session-token"}
}

func endpointMeAccount() domain.Account {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return domain.Account{
		ID:        uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21"),
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Role:      "member",
		CreatedAt: created,
		UpdatedAt: created.Add(48 * time.Hour),
	}
}

func endpointMeDecode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode account body %q: %v", body, err)
	}
	return payload
}

func TestEndpointMeSignedInSelf(t *testing.T) {
	account := endpointMeAccount()
	auth := newStubBackendAuth(t, &stubBackendAuth{account: account}, "https://example.test")
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	response, body := endpointMeDo(t, server, http.MethodGet, []*http.Cookie{endpointMeSessionCookie()})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	payload := endpointMeDecode(t, body)
	publicFields := map[string]bool{
		"id": true, "first_name": true, "last_name": true,
		"email": true, "role": true, "created_at": true, "updated_at": true,
	}
	if len(payload) != len(publicFields) {
		t.Fatalf("account exposes %d fields: %v", len(payload), payload)
	}
	for key := range payload {
		if !publicFields[key] {
			t.Fatalf("account exposes non-public field %q: %v", key, payload)
		}
	}
	if payload["id"] != account.ID.String() || payload["first_name"] != "Ada" || payload["last_name"] != "Lovelace" ||
		payload["email"] != "ada@example.com" || payload["role"] != "member" {
		t.Fatalf("account payload %v", payload)
	}
	created, err := time.Parse(time.RFC3339Nano, payload["created_at"].(string))
	if err != nil || !created.Equal(account.CreatedAt) {
		t.Fatalf("created_at %v (%v)", payload["created_at"], err)
	}
	updated, err := time.Parse(time.RFC3339Nano, payload["updated_at"].(string))
	if err != nil || !updated.Equal(account.UpdatedAt) {
		t.Fatalf("updated_at %v (%v)", payload["updated_at"], err)
	}
	for _, key := range []string{"google_sub", "token", "session", "hash", "sessions"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("account exposes private field %q: %v", key, payload)
		}
	}
}

func TestEndpointMeRequiresContext(t *testing.T) {
	backend := NewBackend(nil, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	handler := endpointMethods(map[string]http.Handler{http.MethodGet: http.HandlerFunc(backend.me)})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	assertBackendError(t, recorder, http.StatusUnauthorized, "unauthenticated")
}

func TestEndpointMeMissingSession(t *testing.T) {
	service := &stubBackendAuth{account: endpointMeAccount()}
	auth := newStubBackendAuth(t, service, "https://example.test")
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	response, body := endpointMeDo(t, server, http.MethodGet, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Error.Code != "unauthenticated" {
		t.Fatalf("body %q (%v)", body, err)
	}
	if service.callCount() != 0 {
		t.Fatalf("sessionless request resolved the account %d times", service.callCount())
	}
}

func TestEndpointMeExpiredSession(t *testing.T) {
	auth := newStubBackendAuth(t, &stubBackendAuth{err: domain.ErrUnauthenticated}, "https://example.test")
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	response, body := endpointMeDo(t, server, http.MethodGet, []*http.Cookie{endpointMeSessionCookie()})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("ada@example.com")) {
		t.Fatalf("expired session body leaked account data: %s", body)
	}
}

func TestEndpointMeBackendFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &stubBackendAuth{err: errors.New("connection string password rejected")}
	auth := NewAuth(nil, service, AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, logger))
	response, body := endpointMeDo(t, server, http.MethodGet, []*http.Cookie{endpointMeSessionCookie()})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("password")) {
		t.Fatalf("backend failure leaked detail: %s", body)
	}
	if bytes.Contains(logs.Bytes(), []byte("password")) {
		t.Fatalf("logs leaked backend detail: %s", logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
}

func TestEndpointMeDuplicateCookies(t *testing.T) {
	service := &stubBackendAuth{account: endpointMeAccount()}
	auth := newStubBackendAuth(t, service, "https://example.test")
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	cookies := []*http.Cookie{endpointMeSessionCookie(), {Name: "__Host-caus_session", Value: "other-token"}}
	response, body := endpointMeDo(t, server, http.MethodGet, cookies)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d (%s)", response.StatusCode, body)
	}
	if service.callCount() != 0 {
		t.Fatalf("duplicate cookies resolved the account %d times", service.callCount())
	}
}

func TestEndpointMeHeadAndWrongMethod(t *testing.T) {
	auth := newStubBackendAuth(t, &stubBackendAuth{account: endpointMeAccount()}, "https://example.test")
	server := newEndpointMeServer(t, auth, NewBackend(nil, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))

	head, body := endpointMeDo(t, server, http.MethodHead, []*http.Cookie{endpointMeSessionCookie()})
	if head.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status %d", head.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD returned a body: %s", body)
	}

	wrong, body := endpointMeDo(t, server, http.MethodPost, []*http.Cookie{endpointMeSessionCookie()})
	if wrong.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d (%s)", wrong.StatusCode, body)
	}
	if allow := wrong.Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("POST Allow %q", allow)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Error.Code != "method_not_allowed" {
		t.Fatalf("POST body %q (%v)", body, err)
	}
}

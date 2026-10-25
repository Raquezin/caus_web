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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointUsersListService struct {
	BackendService
	calls  int
	query  string
	page   domain.Page
	result domain.UserPage
	err    error
}

func (s *endpointUsersListService) ListUsers(_ context.Context, query string, page domain.Page) (domain.UserPage, error) {
	s.calls++
	s.query, s.page = query, page
	return s.result, s.err
}

func newEndpointUsersListHandler(auth *Auth, backend *Backend) http.Handler {
	return auth.requireAdmin(endpointMethods(map[string]http.Handler{
		http.MethodGet: http.HandlerFunc(backend.listUsers),
	}))
}

func endpointUsersListServe(t *testing.T, handler http.Handler, rawQuery string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/users"+rawQuery, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func endpointUsersListAccount() domain.Account {
	return domain.Account{
		ID:        uuid.MustParse("6f4b2e2a-8f0b-4f5a-9c7c-2b1d3e4f5a6b"),
		FirstName: "Admin",
		LastName:  "User",
		Email:     "admin@example.com",
		Role:      "admin",
	}
}

func endpointUsersListAdminAuth(t *testing.T) *Auth {
	t.Helper()
	return newStubBackendAuth(t, &stubBackendAuth{account: endpointUsersListAccount()}, "https://example.test")
}

func endpointUsersListUsers() []domain.User {
	created := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	email := "ada@example.com"
	return []domain.User{
		{
			ID:        uuid.MustParse("9b0f7f0e-1a2b-4c3d-8e9f-0a1b2c3d4e5f"),
			FirstName: "Ada",
			LastName:  "Lovelace",
			Email:     &email,
			Role:      "admin",
			CreatedAt: created,
			UpdatedAt: created.Add(time.Hour),
		},
		{
			ID:        uuid.MustParse("1c2d3e4f-5a6b-4c7d-8e9f-0a1b2c3d4e60"),
			FirstName: "Alan",
			LastName:  "Turing",
			Role:      "member",
			CreatedAt: created,
			UpdatedAt: created,
		},
	}
}

func endpointUsersListDecode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode users body %q: %v", body, err)
	}
	return payload
}

func TestEndpointUsersListSearch(t *testing.T) {
	users := endpointUsersListUsers()
	service := &endpointUsersListService{result: domain.UserPage{Users: users, HasMore: true}}
	auth := endpointUsersListAdminAuth(t)
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	handler := newEndpointUsersListHandler(auth, backend)

	recorder := endpointUsersListServe(t, handler, "?q=%20Ada%20&limit=2&offset=1",
		&http.Cookie{Name: "__Host-caus_session", Value: "admin-token"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type %q", contentType)
	}
	if service.calls != 1 || service.query != "Ada" || service.page != (domain.Page{Limit: 2, Offset: 1}) {
		t.Fatalf("service got query %q page %+v after %d calls", service.query, service.page, service.calls)
	}
	payload := endpointUsersListDecode(t, recorder.Body.Bytes())
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data %v", payload["data"])
	}
	pagination, ok := payload["pagination"].(map[string]any)
	if !ok || pagination["limit"] != float64(2) || pagination["offset"] != float64(1) || pagination["has_more"] != true {
		t.Fatalf("pagination %v", payload["pagination"])
	}
	publicFields := map[string]bool{
		"id": true, "first_name": true, "last_name": true,
		"email": true, "role": true, "created_at": true, "updated_at": true,
	}
	for index, entry := range data {
		user, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("user %d is %T", index, entry)
		}
		if len(user) != len(publicFields) {
			t.Fatalf("user exposes %d fields: %v", len(user), user)
		}
		for key := range user {
			if !publicFields[key] {
				t.Fatalf("user exposes non-public field %q: %v", key, user)
			}
		}
		if user["id"] != users[index].ID.String() || user["role"] != users[index].Role {
			t.Fatalf("user %d payload %v", index, user)
		}
	}
	first := data[0].(map[string]any)
	if first["first_name"] != "Ada" || first["last_name"] != "Lovelace" || first["email"] != "ada@example.com" {
		t.Fatalf("first user %v", first)
	}
	second := data[1].(map[string]any)
	if second["email"] != nil {
		t.Fatalf("nullable email rendered as %v", second["email"])
	}
}

func TestEndpointUsersListEmptyQueryAndResult(t *testing.T) {
	service := &endpointUsersListService{result: domain.UserPage{Users: []domain.User{}}}
	auth := endpointUsersListAdminAuth(t)
	backend := NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	recorder := endpointUsersListServe(t, newEndpointUsersListHandler(auth, backend), "",
		&http.Cookie{Name: "__Host-caus_session", Value: "admin-token"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	if service.query != "" || service.page != (domain.Page{Limit: domain.DefaultPageLimit}) {
		t.Fatalf("service got query %q page %+v", service.query, service.page)
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"data":[]`)) {
		t.Fatalf("empty result body %s", recorder.Body.String())
	}
}

func TestEndpointUsersListRejectsBadInputBeforeService(t *testing.T) {
	rawQueries := []string{
		"?q=Ada&q=Grace",
		"?q=%zz",
		"?q=%ff",
		"?q=%00",
		"?q=Ada%00Lovelace",
		"?q=%01",
		"?q=" + strings.Repeat("a", domain.MaxUserSearchLength+1),
		"?limit=0",
		"?limit=" + strings.Repeat("1", 4),
		"?limit=abc",
		"?limit=2&limit=3",
		"?offset=-1",
		"?offset=abc",
	}
	service := &endpointUsersListService{result: domain.UserPage{Users: []domain.User{}}}
	auth := endpointUsersListAdminAuth(t)
	handler := newEndpointUsersListHandler(auth, NewBackend(service, auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	for _, rawQuery := range rawQueries {
		recorder := endpointUsersListServe(t, handler, rawQuery, &http.Cookie{Name: "__Host-caus_session", Value: "admin-token"})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("query %q status %d (%s)", rawQuery, recorder.Code, recorder.Body.String())
		}
	}
	if service.calls != 0 {
		t.Fatalf("invalid input reached the service %d times", service.calls)
	}
}

func TestEndpointUsersListRequiresAdmin(t *testing.T) {
	service := &endpointUsersListService{result: domain.UserPage{Users: []domain.User{}}}
	memberAuth := newStubBackendAuth(t, &stubBackendAuth{account: domain.Account{
		ID: uuid.New(), Email: "member@example.com", Role: "member",
	}}, "https://example.test")
	memberHandler := newEndpointUsersListHandler(memberAuth,
		NewBackend(service, memberAuth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
	recorder := endpointUsersListServe(t, memberHandler, "", &http.Cookie{Name: "__Host-caus_session", Value: "member-token"})
	assertBackendError(t, recorder, http.StatusForbidden, "forbidden")

	cases := []struct {
		name    string
		auth    *Auth
		cookies []*http.Cookie
	}{
		{"missing", endpointUsersListAdminAuth(t), nil},
		{"duplicate", endpointUsersListAdminAuth(t), []*http.Cookie{
			{Name: "__Host-caus_session", Value: "one"},
			{Name: "__Host-caus_session", Value: "two"},
		}},
		{"expired", newStubBackendAuth(t, &stubBackendAuth{err: domain.ErrUnauthenticated}, "https://example.test"),
			[]*http.Cookie{{Name: "__Host-caus_session", Value: "expired-token"}}},
	}
	for _, testCase := range cases {
		handler := newEndpointUsersListHandler(testCase.auth,
			NewBackend(service, testCase.auth, slog.New(slog.NewJSONHandler(io.Discard, nil))))
		recorder := endpointUsersListServe(t, handler, "", testCase.cookies...)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s session status %d (%s)", testCase.name, recorder.Code, recorder.Body.String())
		}
	}
	if service.calls != 0 {
		t.Fatalf("unauthenticated requests reached the service %d times", service.calls)
	}
}

func TestEndpointUsersListSanitizesFailure(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &endpointUsersListService{err: errors.New("database password rejected")}
	auth := NewAuth(nil, &stubBackendAuth{account: endpointUsersListAccount()},
		AuthConfig{PublicURL: "https://example.test", SessionTTL: time.Hour}, logger)
	handler := newEndpointUsersListHandler(auth, NewBackend(service, auth, logger))
	recorder := endpointUsersListServe(t, handler, "?q=Ada", &http.Cookie{Name: "__Host-caus_session", Value: "admin-token"})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d (%s)", recorder.Code, recorder.Body.String())
	}
	assertBackendError(t, recorder, http.StatusInternalServerError, "internal_error")
	if bytes.Contains(recorder.Body.Bytes(), []byte("password")) {
		t.Fatalf("failure leaked detail: %s", recorder.Body.String())
	}
	if bytes.Contains(logs.Bytes(), []byte("password")) {
		t.Fatalf("logs leaked detail: %s", logs.String())
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", logs.String())
	}
}

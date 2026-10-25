package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestBackendIntegrationPeopleAdminOnly(t *testing.T) {
	f := newIntegrationFixture(t)
	id := f.insertPerson("Private", "LoginOnly", "private-login@example.test", "audit-private-sub", "member")

	status, _, body := f.public(http.MethodGet, "/api/people")
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	status, _, body = f.public(http.MethodGet, "/api/people/"+id.String())
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")

	for _, path := range []string{"/api/people", "/api/people/" + id.String()} {
		status, _, body = f.do(integrationRequest{method: http.MethodGet, path: path, token: f.memberToken})
		integrationExpect(t, status, body, http.StatusForbidden, "forbidden")
	}

	status, _, body = f.admin(http.MethodGet, "/api/people/"+id.String(), nil)
	if status != http.StatusOK {
		t.Fatalf("admin get person: status %d body %s", status, body)
	}
	if !strings.Contains(string(body), "Private") || !strings.Contains(string(body), "LoginOnly") {
		t.Fatalf("admin get person missing names: %s", body)
	}
	if strings.Contains(string(body), "private-login@example.test") || strings.Contains(string(body), "audit-private-sub") {
		t.Fatalf("admin get person leaked email or subject: %s", body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/people", nil)
	if status != http.StatusOK || !strings.Contains(string(body), id.String()) {
		t.Fatalf("admin list people: status %d body %s", status, body)
	}

	token := f.createSession(id)
	status, _, body = f.do(integrationRequest{
		method:  http.MethodDelete,
		path:    "/api/me",
		token:   token,
		origins: []string{f.origin},
	})
	integrationExpect(t, status, body, http.StatusNoContent, "")

	status, _, body = f.public(http.MethodGet, "/api/people/"+id.String())
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: token})
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
}

func TestBackendIntegrationUserSearchNULRejected(t *testing.T) {
	f := newIntegrationFixture(t)
	status, _, body := f.admin(http.MethodGet, "/api/admin/users?q=%00", nil)
	integrationExpect(t, status, body, http.StatusBadRequest, "invalid_query")
}

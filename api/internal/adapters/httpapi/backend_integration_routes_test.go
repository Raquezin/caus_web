package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestBackendRouteMethodAllow(t *testing.T) {
	f := newIntegrationFixture(t)
	id := uuid.NewString()
	routes := []struct{ path, allow string }{
		{"/api/people", "GET, HEAD"},
		{"/api/people/" + id, "GET, HEAD"},
		{"/api/healthz", "GET, HEAD"},
		{"/api/readyz", "GET, HEAD"},
		{"/api/auth/google/login", "GET, HEAD"},
		{"/api/auth/google/callback", "GET, HEAD"},
		{"/api/talks", "GET, HEAD"},
		{"/api/talks/" + id, "GET, HEAD"},
		{"/api/talks.ics", "GET, HEAD"},
		{"/api/competitions", "GET, HEAD"},
		{"/api/competitions/" + id, "GET, HEAD"},
		{"/api/me", "DELETE, GET, HEAD"},
		{"/api/me/export", "GET, HEAD"},
		{"/api/auth/logout", "POST"},
		{"/api/admin/talks", "GET, HEAD, POST"},
		{"/api/admin/talks/" + id, "DELETE, GET, HEAD, PATCH"},
		{"/api/admin/competitions", "GET, HEAD, POST"},
		{"/api/admin/competitions/" + id, "DELETE, GET, HEAD, PATCH"},
		{"/api/admin/competitions/" + id + "/teams", "GET, HEAD, POST"},
		{"/api/admin/teams/" + id, "DELETE, GET, HEAD, PATCH"},
		{"/api/admin/users", "GET, HEAD"},
		{"/api/admin/users/" + id, "PATCH"},
	}
	for _, route := range routes {
		status, header, body := f.public(http.MethodPut, route.path)
		integrationExpect(t, status, body, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := header.Get("Allow"); allow != route.allow {
			t.Fatalf("PUT %s: Allow %q, want %q", route.path, allow, route.allow)
		}
	}
	status, header, body := f.public(http.MethodGet, "/api/admin/users/"+id)
	integrationExpect(t, status, body, http.StatusMethodNotAllowed, "method_not_allowed")
	if allow := header.Get("Allow"); allow != "PATCH" {
		t.Fatalf("GET admin user: Allow %q", allow)
	}
	status, _, body = f.public(http.MethodGet, "/api/unknown")
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
}

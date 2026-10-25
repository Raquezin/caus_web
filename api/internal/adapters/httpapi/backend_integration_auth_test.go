package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestBackendIntegrationProtectedRoutes(t *testing.T) {
	f := newIntegrationFixture(t)
	id := uuid.NewString()
	admin := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/talks"},
		{http.MethodPost, "/api/admin/talks"},
		{http.MethodGet, "/api/admin/talks/" + id},
		{http.MethodPatch, "/api/admin/talks/" + id},
		{http.MethodDelete, "/api/admin/talks/" + id},
		{http.MethodGet, "/api/admin/competitions"},
		{http.MethodPost, "/api/admin/competitions"},
		{http.MethodGet, "/api/admin/competitions/" + id},
		{http.MethodPatch, "/api/admin/competitions/" + id},
		{http.MethodDelete, "/api/admin/competitions/" + id},
		{http.MethodGet, "/api/admin/competitions/" + id + "/teams"},
		{http.MethodPost, "/api/admin/competitions/" + id + "/teams"},
		{http.MethodGet, "/api/admin/teams/" + id},
		{http.MethodPatch, "/api/admin/teams/" + id},
		{http.MethodDelete, "/api/admin/teams/" + id},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodPatch, "/api/admin/users/" + id},
	}
	if len(admin) != 17 {
		t.Fatalf("admin route matrix has %d entries, want 17", len(admin))
	}
	for _, endpoint := range admin {
		status, _, body := f.public(endpoint.method, endpoint.path)
		integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
		status, _, body = f.memberRequest(endpoint.method, endpoint.path, nil)
		integrationExpect(t, status, body, http.StatusForbidden, "forbidden")
	}
	user := []struct{ method, path string }{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/me/export"},
		{http.MethodPost, "/api/auth/logout"},
		{http.MethodDelete, "/api/me"},
	}
	expired := f.createExpiredSession(f.memberID)
	for _, endpoint := range user {
		status, _, body := f.public(endpoint.method, endpoint.path)
		integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
		status, _, body = f.do(integrationRequest{method: endpoint.method, path: endpoint.path, token: expired})
		integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
		status, _, body = f.do(integrationRequest{
			method:  endpoint.method,
			path:    endpoint.path,
			headers: map[string]string{"Cookie": "caus_session=" + f.memberToken + "; caus_session=" + f.memberToken},
		})
		integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	}
}

func rejectedOrigins(publicOrigin string) []struct {
	origins []string
	code    string
} {
	return []struct {
		origins []string
		code    string
	}{
		{nil, "origin_denied"},
		{[]string{"https://evil.example"}, "cors_origin_denied"},
		{[]string{"https://clubalgoritmiaus.es"}, "cors_origin_denied"},
		{[]string{publicOrigin, publicOrigin}, "origin_denied"},
	}
}

func TestBackendIntegrationWriteOrigins(t *testing.T) {
	f := newIntegrationFixture(t)
	id := uuid.NewString()
	writes := []struct{ method, path string }{
		{http.MethodPost, "/api/admin/talks"},
		{http.MethodPatch, "/api/admin/talks/" + id},
		{http.MethodDelete, "/api/admin/talks/" + id},
		{http.MethodPost, "/api/admin/competitions"},
		{http.MethodPatch, "/api/admin/competitions/" + id},
		{http.MethodDelete, "/api/admin/competitions/" + id},
		{http.MethodPost, "/api/admin/competitions/" + id + "/teams"},
		{http.MethodPatch, "/api/admin/teams/" + id},
		{http.MethodDelete, "/api/admin/teams/" + id},
		{http.MethodPatch, "/api/admin/users/" + id},
	}
	for _, endpoint := range writes {
		for _, testCase := range rejectedOrigins(f.origin) {
			status, _, body := f.do(integrationRequest{
				method: endpoint.method, path: endpoint.path, token: f.adminToken, origins: testCase.origins,
			})
			integrationExpect(t, status, body, http.StatusForbidden, testCase.code)
		}
	}
	userWrites := []struct {
		method, path, token string
	}{
		{http.MethodDelete, "/api/me", f.memberToken},
		{http.MethodPost, "/api/auth/logout", f.memberToken},
	}
	for _, endpoint := range userWrites {
		for _, testCase := range rejectedOrigins(f.origin) {
			status, _, body := f.do(integrationRequest{
				method: endpoint.method, path: endpoint.path, token: endpoint.token, origins: testCase.origins,
			})
			integrationExpect(t, status, body, http.StatusForbidden, testCase.code)
		}
	}
	if talks := f.count("SELECT count(*) FROM talks"); talks != 0 {
		t.Fatalf("rejected writes created %d talks", talks)
	}
	if competitions := f.count("SELECT count(*) FROM competitions"); competitions != 0 {
		t.Fatalf("rejected writes created %d competitions", competitions)
	}
	if sessions := f.count("SELECT count(*) FROM sessions WHERE person_id = $1", f.memberID); sessions != 1 {
		t.Fatalf("rejected logout removed the member session: %d rows", sessions)
	}
}

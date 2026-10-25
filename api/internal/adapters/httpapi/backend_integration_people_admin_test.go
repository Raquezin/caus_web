package httpapi

import (
	"net/http"
	"testing"
)

func TestBackendIntegrationPeopleDirectoryRequiresAdmin(t *testing.T) {
	f := newIntegrationFixture(t)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/api/people", "/api/people?limit=100", "/api/people/" + f.memberID.String()} {
			status, _, body := f.public(method, path)
			if method == http.MethodHead {
				if status != http.StatusUnauthorized {
					t.Fatalf("HEAD %s: status %d, want 401", path, status)
				}
				continue
			}
			integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
		}
	}
	for _, path := range []string{"/api/people", "/api/people/" + f.memberID.String()} {
		status, _, body := f.do(integrationRequest{method: http.MethodGet, path: path, token: f.memberToken})
		integrationExpect(t, status, body, http.StatusForbidden, "forbidden")
	}

	status, _, body := f.do(integrationRequest{method: http.MethodGet, path: "/api/people?limit=100", token: f.adminToken})
	if status != http.StatusOK {
		t.Fatalf("admin list: status %d body %s", status, body)
	}
	var listed bool
	for _, item := range integrationData(t, body) {
		person := item.(map[string]any)
		if person["id"] == f.memberID.String() {
			listed = true
			if person["first_name"] != "Grace" || person["last_name"] != "Member" {
				t.Fatalf("member entry %v", person)
			}
		}
	}
	if !listed {
		t.Fatalf("admin list missing member: %s", body)
	}
	status, _, body = f.do(integrationRequest{
		method: http.MethodGet, path: "/api/people/" + f.memberID.String(), token: f.adminToken})
	if status != http.StatusOK || integrationJSON(t, body)["first_name"] != "Grace" {
		t.Fatalf("admin detail: status %d body %s", status, body)
	}

	status, _, body = f.public(http.MethodGet, "/api/people/not-a-uuid")
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
	status, _, body = f.public(http.MethodGet, "/api/people?limit=x")
	integrationExpect(t, status, body, http.StatusUnauthorized, "unauthenticated")
}

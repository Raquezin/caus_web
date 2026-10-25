package httpapi

import (
	"net/http"
	"testing"
)

func TestBackendIntegrationUserRoles(t *testing.T) {
	f := newIntegrationFixture(t)
	hopper := f.insertPerson("Grace", "Hopper", "grace.hopper@example.test", "sub-hopper", "member")
	alan := f.insertPerson("Alan", "Turing", "alan@example.test", "sub-alan", "member")

	userIDs := func(body []byte) []string {
		t.Helper()
		ids := make([]string, 0)
		for _, entry := range integrationData(t, body) {
			user := entry.(map[string]any)
			for _, private := range []string{"google_sub", "token_hash"} {
				if _, ok := user[private]; ok {
					t.Fatalf("user entry exposed %q: %v", private, user)
				}
			}
			ids = append(ids, user["id"].(string))
		}
		return ids
	}

	status, _, body := f.admin(http.MethodGet, "/api/admin/users?q=Grace", nil)
	if status != http.StatusOK {
		t.Fatalf("search: status %d body %s", status, body)
	}
	ids := userIDs(body)
	if len(ids) != 2 {
		t.Fatalf("search Grace returned %v", ids)
	}
	seen := map[string]bool{ids[0]: true, ids[1]: true}
	if !seen[f.memberID.String()] || !seen[hopper.String()] || seen[alan.String()] {
		t.Fatalf("search Grace returned %v", ids)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=grace%20member", nil)
	if status != http.StatusOK || len(userIDs(body)) != 1 || userIDs(body)[0] != f.memberID.String() {
		t.Fatalf("full name search: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=member%40example.test", nil)
	if status != http.StatusOK || len(userIDs(body)) != 1 || userIDs(body)[0] != f.memberID.String() {
		t.Fatalf("email search: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=%25", nil)
	if status != http.StatusOK || len(userIDs(body)) != 0 {
		t.Fatalf("wildcard search: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=Grace&limit=1", nil)
	if status != http.StatusOK {
		t.Fatalf("paged search: status %d body %s", status, body)
	}
	firstPage := integrationJSON(t, body)
	if len(firstPage["data"].([]any)) != 1 || firstPage["pagination"].(map[string]any)["has_more"] != true {
		t.Fatalf("first page %v", firstPage)
	}
	firstID := firstPage["data"].([]any)[0].(map[string]any)["id"]
	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=Grace&limit=1&offset=1", nil)
	if status != http.StatusOK {
		t.Fatalf("second page: status %d body %s", status, body)
	}
	secondPage := integrationJSON(t, body)
	secondData := secondPage["data"].([]any)
	if len(secondData) != 1 || secondPage["pagination"].(map[string]any)["has_more"] != false {
		t.Fatalf("second page %v", secondPage)
	}
	if secondData[0].(map[string]any)["id"] == firstID {
		t.Fatalf("pagination repeated user %v", firstID)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/users/"+f.memberID.String(), map[string]any{"role": "admin"})
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "admin" {
		t.Fatalf("promote: status %d body %s", status, body)
	}
	status, _, body = f.memberRequest(http.MethodGet, "/api/me", nil)
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "admin" {
		t.Fatalf("promoted session /api/me: status %d body %s", status, body)
	}
	status, _, body = f.memberRequest(http.MethodGet, "/api/admin/users?q=Grace", nil)
	if status != http.StatusOK {
		t.Fatalf("promoted session admin request: status %d body %s", status, body)
	}
	status, _, body = f.memberRequest(http.MethodPatch, "/api/admin/users/"+f.memberID.String(), map[string]any{"role": "member"})
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "member" {
		t.Fatalf("demote: status %d body %s", status, body)
	}
	status, _, body = f.memberRequest(http.MethodGet, "/api/me", nil)
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "member" {
		t.Fatalf("demoted session /api/me: status %d body %s", status, body)
	}
	status, _, body = f.memberRequest(http.MethodGet, "/api/admin/users", nil)
	integrationExpect(t, status, body, http.StatusForbidden, "forbidden")
	status, _, body = f.admin(http.MethodGet, "/api/me", nil)
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "admin" {
		t.Fatalf("original admin demoted: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodGet, "/api/admin/users?limit=abc", nil)
	integrationExpect(t, status, body, http.StatusBadRequest, "invalid_pagination")
	status, _, body = f.admin(http.MethodGet, "/api/admin/users?q=a&q=b", nil)
	integrationExpect(t, status, body, http.StatusBadRequest, "invalid_query")
}

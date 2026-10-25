package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestEndpointUserRoleRejectsMalformedRequest(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	target := "/api/admin/users/" + fixture.memberID.String()
	origin := []string{"https://example.test"}
	cases := []struct {
		name   string
		body   string
		status int
		code   string
		field  string
	}{
		{"missing role", `{}`, http.StatusUnprocessableEntity, "validation_failed", "role"},
		{"null role", `{"role":null}`, http.StatusUnprocessableEntity, "validation_failed", "role"},
		{"numeric role", `{"role":7}`, http.StatusUnprocessableEntity, "validation_failed", "role"},
		{"array role", `{"role":["admin"]}`, http.StatusUnprocessableEntity, "validation_failed", "role"},
		{"empty body", ``, http.StatusBadRequest, "invalid_body", ""},
		{"null body", `null`, http.StatusBadRequest, "invalid_body", ""},
		{"unknown field", `{"role":"admin","email":"ada@example.com"}`, http.StatusBadRequest, "invalid_body", ""},
		{"multiple values", `{"role":"admin"}{"role":"member"}`, http.StatusBadRequest, "invalid_body", ""},
	}
	for _, testCase := range cases {
		response, body := fixture.do(t, http.MethodPatch, target, testCase.body, "admin-session", origin)
		if response.StatusCode != testCase.status {
			t.Fatalf("%s status %d, want %d (%s)", testCase.name, response.StatusCode, testCase.status, body)
		}
		if code := endpointUserRoleCode(t, body); code != testCase.code {
			t.Fatalf("%s code %q, want %q", testCase.name, code, testCase.code)
		}
		if testCase.field != "" {
			if fields := endpointUserRoleFields(t, body); fields[testCase.field] == nil {
				t.Fatalf("%s missing field error: %s", testCase.name, body)
			}
		}
	}
	response, body := fixture.do(t, http.MethodPatch, "/api/admin/users/not-a-uuid", `{"role":"admin"}`, "admin-session", origin)
	if response.StatusCode != http.StatusBadRequest || endpointUserRoleCode(t, body) != "invalid_id" {
		t.Fatalf("invalid id status %d (%s)", response.StatusCode, body)
	}
	if fixture.service.calls != 0 {
		t.Fatalf("malformed requests reached UpdateUserRole %d times", fixture.service.calls)
	}
}

func TestEndpointUserRoleRejectsInvalidRole(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	target := "/api/admin/users/" + fixture.memberID.String()
	for _, role := range []string{"", "owner", "ADMIN", "admin "} {
		response, body := fixture.do(t, http.MethodPatch, target, `{"role":"`+role+`"}`, "admin-session",
			[]string{"https://example.test"})
		if response.StatusCode != http.StatusUnprocessableEntity || endpointUserRoleCode(t, body) != "validation_failed" {
			t.Fatalf("role %q status %d (%s)", role, response.StatusCode, body)
		}
		if fields := endpointUserRoleFields(t, body); fields["role"] == nil {
			t.Fatalf("role %q missing field error: %s", role, body)
		}
	}
	if fixture.service.calls != 4 {
		t.Fatalf("invalid roles reached the service %d times", fixture.service.calls)
	}
}

func TestEndpointUserRoleMissingUser(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	response, body := fixture.do(t, http.MethodPatch, "/api/admin/users/"+uuid.NewString(), `{"role":"admin"}`,
		"admin-session", []string{"https://example.test"})
	if response.StatusCode != http.StatusNotFound || endpointUserRoleCode(t, body) != "not_found" {
		t.Fatalf("missing user status %d (%s)", response.StatusCode, body)
	}
}

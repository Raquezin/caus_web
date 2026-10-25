package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"testing"
)

func TestEndpointUserRoleGuards(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	target := "/api/admin/users/" + fixture.memberID.String()
	origin := "https://example.test"
	cases := []struct {
		name    string
		token   string
		origins []string
		status  int
		code    string
	}{
		{"missing session", "", []string{origin}, http.StatusUnauthorized, "unauthenticated"},
		{"member", "member-session", []string{origin}, http.StatusForbidden, "forbidden"},
		{"missing origin", "admin-session", nil, http.StatusForbidden, "origin_denied"},
		{"wrong origin", "admin-session", []string{"https://attacker.test"}, http.StatusForbidden, "origin_denied"},
		{"duplicate origin", "admin-session", []string{origin, origin}, http.StatusForbidden, "origin_denied"},
		{"prefixed origin", "admin-session", []string{origin + ".evil.test"}, http.StatusForbidden, "origin_denied"},
	}
	for _, testCase := range cases {
		response, body := fixture.do(t, http.MethodPatch, target, `{"role":"admin"}`, testCase.token, testCase.origins)
		if response.StatusCode != testCase.status || endpointUserRoleCode(t, body) != testCase.code {
			t.Fatalf("%s status %d code %q (%s)", testCase.name, response.StatusCode, endpointUserRoleCode(t, body), body)
		}
	}
	if fixture.service.calls != 0 {
		t.Fatalf("guarded requests reached UpdateUserRole %d times", fixture.service.calls)
	}
	response, body := fixture.do(t, http.MethodPatch, target, `{"role":"admin"}`, "admin-session", []string{origin})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin status %d (%s)", response.StatusCode, body)
	}
	if fixture.service.calls != 1 {
		t.Fatalf("admin request calls %d", fixture.service.calls)
	}
}

func TestEndpointUserRoleBackendFailure(t *testing.T) {
	fixture := newEndpointUserRoleFixture(t)
	fixture.service.err = errors.New("role update failed: password authentication failed")
	response, body := fixture.do(t, http.MethodPatch, "/api/admin/users/"+fixture.memberID.String(), `{"role":"admin"}`,
		"admin-session", []string{"https://example.test"})
	if response.StatusCode != http.StatusInternalServerError || endpointUserRoleCode(t, body) != "internal_error" {
		t.Fatalf("failure status %d (%s)", response.StatusCode, body)
	}
	if bytes.Contains(body, []byte("password")) || bytes.Contains(fixture.logs.Bytes(), []byte("password")) {
		t.Fatalf("failure leaked detail: body %s logs %s", body, fixture.logs.String())
	}
	if !bytes.Contains(fixture.logs.Bytes(), []byte(`"error_type"`)) {
		t.Fatalf("logs missing error type: %s", fixture.logs.String())
	}
}

package httpapi

import (
	"context"
	"net/http"
	"testing"
)

func TestBackendIntegrationLastAdminCannotDemoteItself(t *testing.T) {
	f := newIntegrationFixture(t)
	status, _, body := f.admin(http.MethodPatch, "/api/admin/users/"+f.adminID.String(), map[string]any{"role": "member"})
	integrationExpect(t, status, body, http.StatusConflict, "conflict")
	if admins := f.count("SELECT count(*) FROM people WHERE role = 'admin'"); admins != 1 {
		t.Fatalf("admins after rejected demotion %d, want 1", admins)
	}
	status, _, body = f.admin(http.MethodGet, "/api/me", nil)
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "admin" {
		t.Fatalf("admin session after rejected demotion: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/users", nil)
	if status != http.StatusOK {
		t.Fatalf("admin request after rejected demotion: status %d body %s", status, body)
	}
}

func TestBackendIntegrationLastAdminCannotDeleteAccount(t *testing.T) {
	f := newIntegrationFixture(t)
	status, header, body := f.admin(http.MethodDelete, "/api/me", nil)
	integrationExpect(t, status, body, http.StatusConflict, "conflict")
	if cookie := header.Get("Set-Cookie"); cookie != "" {
		t.Fatalf("rejected deletion cleared the session cookie %q", cookie)
	}
	if admins := f.count("SELECT count(*) FROM people WHERE role = 'admin'"); admins != 1 {
		t.Fatalf("admins after rejected deletion %d, want 1", admins)
	}
	var email, subject *string
	var role string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT email, google_sub, role::text FROM people WHERE id = $1", f.adminID).Scan(&email, &subject, &role); err != nil {
		t.Fatal(err)
	}
	if email == nil || *email != "admin@example.test" || subject == nil || *subject != "sub-admin" || role != "admin" {
		t.Fatalf("admin after rejected deletion email=%v sub=%v role=%q", email, subject, role)
	}
	if sessions := f.count("SELECT count(*) FROM sessions WHERE person_id = $1", f.adminID); sessions == 0 {
		t.Fatal("rejected deletion removed the admin sessions")
	}
	status, _, body = f.admin(http.MethodGet, "/api/me", nil)
	if status != http.StatusOK || integrationJSON(t, body)["role"] != "admin" {
		t.Fatalf("admin session after rejected deletion: status %d body %s", status, body)
	}
}

func TestBackendIntegrationDeletedAccountCannotBecomeAdmin(t *testing.T) {
	f := newIntegrationFixture(t)
	status, _, body := f.memberRequest(http.MethodDelete, "/api/me", nil)
	integrationExpect(t, status, body, http.StatusNoContent, "")
	status, _, body = f.admin(http.MethodPatch, "/api/admin/users/"+f.memberID.String(), map[string]any{"role": "admin"})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "role")
	if admins := f.count("SELECT count(*) FROM people WHERE role = 'admin'"); admins != 1 {
		t.Fatalf("admins after rejected promotion %d, want 1", admins)
	}
	status, _, body = f.admin(http.MethodDelete, "/api/me", nil)
	integrationExpect(t, status, body, http.StatusConflict, "conflict")
}

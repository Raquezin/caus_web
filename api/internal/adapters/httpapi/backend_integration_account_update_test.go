package httpapi

import (
	"context"
	"net/http"
	"testing"
)

func TestBackendIntegrationAccountUpdate(t *testing.T) {
	f := newIntegrationFixture(t)

	status, _, body := f.memberRequest(http.MethodPatch, "/api/me",
		map[string]any{"first_name": "  Grace ", "last_name": ""})
	if status != http.StatusOK {
		t.Fatalf("update account: status %d body %s", status, body)
	}
	account := integrationJSON(t, body)
	if account["id"] != f.memberID.String() || account["first_name"] != "Grace" || account["last_name"] != "" {
		t.Fatalf("updated account %v", account)
	}

	status, _, body = f.do(integrationRequest{method: http.MethodGet, path: "/api/me", token: f.memberToken})
	if status != http.StatusOK {
		t.Fatalf("read account: status %d body %s", status, body)
	}
	current := integrationJSON(t, body)
	if current["first_name"] != "Grace" || current["last_name"] != "" {
		t.Fatalf("account after update %v", current)
	}

	var adminFirst, adminLast string
	if err := f.pool.QueryRow(context.Background(),
		"SELECT first_name, last_name FROM people WHERE id = $1", f.adminID).Scan(&adminFirst, &adminLast); err != nil {
		t.Fatal(err)
	}
	if adminFirst != "Ada" || adminLast != "Admin" {
		t.Fatalf("admin names changed to %q %q", adminFirst, adminLast)
	}

	status, _, body = f.memberRequest(http.MethodPatch, "/api/me", map[string]any{})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
}

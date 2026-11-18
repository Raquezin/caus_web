package httpapi

import (
	"math/rand"
	"net/http"
	"testing"
)

func integrationNoise(source *rand.Rand, length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, length)
	for index := range out {
		out[index] = alphabet[source.Intn(len(alphabet))]
	}
	return string(out)
}

func TestBackendIntegrationTextIsStoredTrimmed(t *testing.T) {
	f := newIntegrationFixture(t)
	competition := func(name string) map[string]any {
		return map[string]any{
			"name": name, "description": "concurso", "date": "2027-03-01T10:00:00", "location": "Sevilla",
		}
	}
	status, _, body := f.admin(http.MethodPost, "/api/admin/competitions", competition(" CompliCAUS 2027 "))
	integrationExpect(t, status, body, http.StatusCreated, "")
	if name := integrationJSON(t, body)["name"]; name != "CompliCAUS 2027" {
		t.Fatalf("stored competition name %v, want trimmed", name)
	}
	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions", competition("CompliCAUS 2027 "))
	integrationExpect(t, status, body, http.StatusConflict, "conflict")

	talkID := f.createPublishedTalk("Baseline talk", true)
	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+talkID.String(), map[string]any{
		"title": "  New title\n",
	})
	integrationExpect(t, status, body, http.StatusOK, "")
	if title := integrationJSON(t, body)["title"]; title != "New title" {
		t.Fatalf("stored talk title %v, want trimmed", title)
	}

	competitionID := f.createCompetitionRow("Trim cup", 3)
	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID.String()+"/teams",
		map[string]any{"name": "   ", "member_ids": []string{f.memberID.String()}})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "name")
}

func TestBackendIntegrationLongIndexedTextIsRejected(t *testing.T) {
	f := newIntegrationFixture(t)
	source := rand.New(rand.NewSource(7))
	noise := func() string { return integrationNoise(source, 6000) }
	rejected := func(label string, status int, body []byte, field string) {
		t.Helper()
		if status != http.StatusUnprocessableEntity || integrationErrorCode(t, body) != "validation_failed" {
			t.Fatalf("%s: status %d body %s", label, status, body)
		}
		integrationValidationField(t, body, field)
	}

	talk := validIntegrationTalkBody(f, "Long text baseline")
	talk["title"] = noise()
	status, _, body := f.admin(http.MethodPost, "/api/admin/talks", talk)
	rejected("POST talk title", status, body, "title")

	talk = validIntegrationTalkBody(f, "Long location talk")
	talk["location"] = noise()
	status, _, body = f.admin(http.MethodPost, "/api/admin/talks", talk)
	rejected("POST talk location", status, body, "location")

	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions", map[string]any{
		"name": noise(), "description": "concurso", "date": "2027-04-06T10:00:00", "location": "Sevilla",
	})
	rejected("POST competition name", status, body, "name")

	cupID := f.createCompetitionRow("Long text cup", 2)
	teams := "/api/admin/competitions/" + cupID.String() + "/teams"
	status, _, body = f.admin(http.MethodPost, teams, map[string]any{
		"name": noise(), "member_ids": []string{f.memberID.String()},
	})
	rejected("POST team name", status, body, "name")

	status, _, body = f.admin(http.MethodPost, teams, map[string]any{
		"name": "A", "member_ids": []string{f.memberID.String()},
	})
	integrationExpect(t, status, body, http.StatusCreated, "")
	teamID, _ := integrationJSON(t, body)["id"].(string)
	status, _, body = f.admin(http.MethodPatch, "/api/admin/teams/"+teamID, map[string]any{"name": noise()})
	rejected("PATCH team name", status, body, "name")

	talk = validIntegrationTalkBody(f, "Long description talk")
	talk["description"] = noise()
	status, _, body = f.admin(http.MethodPost, "/api/admin/talks", talk)
	integrationExpect(t, status, body, http.StatusCreated, "")
}

func TestBackendIntegrationRejectsBOMOnlyFirstName(t *testing.T) {
	f := newIntegrationFixture(t)
	status, _, body := f.memberRequest(http.MethodPatch, "/api/me", map[string]any{"first_name": "\uFEFF"})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "first_name")
}

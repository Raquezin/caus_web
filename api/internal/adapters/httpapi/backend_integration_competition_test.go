package httpapi

import (
	"net/http"
	"testing"
)

func TestBackendIntegrationCompetitionTeams(t *testing.T) {
	f := newIntegrationFixture(t)
	teammate := f.insertPerson("Barbara", "Liskov", "barbara@example.test", "sub-barbara", "member")
	status, header, body := f.admin(http.MethodPost, "/api/admin/competitions", map[string]any{
		"name": "Autumn Cup", "description": "concurso", "date": "2026-09-15T10:00:00", "location": "Madrid",
	})
	if status != http.StatusCreated {
		t.Fatalf("create competition: status %d body %s", status, body)
	}
	created := integrationJSON(t, body)
	competitionID, _ := created["id"].(string)
	if created["team_size"] != float64(3) || header.Get("Location") != "/api/admin/competitions/"+competitionID {
		t.Fatalf("create competition body %v location %q", created, header.Get("Location"))
	}
	status, _, body = f.public(http.MethodGet, "/api/competitions/"+competitionID)
	if status != http.StatusOK {
		t.Fatalf("public detail: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/competitions/"+competitionID, nil)
	if status != http.StatusOK {
		t.Fatalf("admin detail: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodPatch, "/api/admin/competitions/"+competitionID, map[string]any{
		"name": "Autumn Cup 2026", "team_size": 3,
	})
	if status != http.StatusOK || integrationJSON(t, body)["name"] != "Autumn Cup 2026" {
		t.Fatalf("patch competition: status %d body %s", status, body)
	}

	members := []string{f.memberID.String(), f.adminID.String(), teammate.String()}
	status, header, body = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", map[string]any{
		"name": "Los Tres", "member_ids": members,
	})
	if status != http.StatusCreated {
		t.Fatalf("create team: status %d body %s", status, body)
	}
	team := integrationJSON(t, body)
	teamID, _ := team["id"].(string)
	if header.Get("Location") != "/api/admin/teams/"+teamID {
		t.Fatalf("create team location %q", header.Get("Location"))
	}
	if entries, ok := team["members"].([]any); !ok || len(entries) != 3 {
		t.Fatalf("create team members %v", team["members"])
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/competitions/"+competitionID+"/teams", nil)
	if status != http.StatusOK || len(integrationData(t, body)) != 1 {
		t.Fatalf("teams list: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/teams/"+teamID, nil)
	if status != http.StatusOK {
		t.Fatalf("team detail: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/teams/"+teamID, map[string]any{
		"name": "Los Tres A", "photo_url": "https://example.com/team.png", "position": 2,
	})
	if status != http.StatusOK {
		t.Fatalf("patch team: status %d body %s", status, body)
	}
	updated := integrationJSON(t, body)
	if updated["name"] != "Los Tres A" || updated["photo_url"] != "https://example.com/team.png" ||
		updated["position"] != float64(2) {
		t.Fatalf("patch team body %v", updated)
	}
	status, _, body = f.admin(http.MethodPatch, "/api/admin/teams/"+teamID, map[string]any{
		"member_ids": []string{f.adminID.String(), teammate.String()},
	})
	if status != http.StatusOK {
		t.Fatalf("patch team membership: status %d body %s", status, body)
	}
	rotated := integrationJSON(t, body)
	if entries, ok := rotated["members"].([]any); !ok || len(entries) != 2 {
		t.Fatalf("patch team membership %v", rotated["members"])
	}
	status, _, body = f.admin(http.MethodPatch, "/api/admin/teams/"+teamID, map[string]any{
		"name": nil, "photo_url": nil, "position": nil,
	})
	if status != http.StatusOK {
		t.Fatalf("patch team nullable: status %d body %s", status, body)
	}
	cleared := integrationJSON(t, body)
	if cleared["name"] != nil || cleared["photo_url"] != nil || cleared["position"] != nil {
		t.Fatalf("patch team nullable body %v", cleared)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/competitions/"+competitionID, map[string]any{
		"name": "Shrunk", "team_size": 1,
	})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	errorObject := integrationJSON(t, body)["error"].(map[string]any)
	fields, _ := errorObject["fields"].(map[string]any)
	if len(fields) == 0 {
		t.Fatalf("shrink error fields %v", errorObject)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/competitions/"+competitionID, nil)
	afterShrink := integrationJSON(t, body)["competition"].(map[string]any)
	if afterShrink["name"] != "Autumn Cup 2026" || afterShrink["team_size"] != float64(3) {
		t.Fatalf("shrink rolled back scalars: %v", afterShrink)
	}

	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", map[string]any{
		"name": "Duplicado", "member_ids": []string{f.adminID.String()},
	})
	integrationExpect(t, status, body, http.StatusConflict, "conflict")

	status, _, body = f.admin(http.MethodDelete, "/api/admin/teams/"+teamID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete team: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/teams/"+teamID, nil)
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	if people := f.count("SELECT count(*) FROM people WHERE id = $1", f.memberID); people != 1 {
		t.Fatal("team delete removed a person")
	}
	status, _, body = f.public(http.MethodGet, "/api/competitions/"+competitionID)
	if status != http.StatusOK {
		t.Fatalf("competition after team delete: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", map[string]any{
		"name": "Cascada", "member_ids": members,
	})
	if status != http.StatusCreated {
		t.Fatalf("create cascade team: status %d body %s", status, body)
	}
	cascadeTeamID, _ := integrationJSON(t, body)["id"].(string)
	if teams := f.count("SELECT count(*) FROM teams WHERE id = $1", cascadeTeamID); teams != 1 {
		t.Fatalf("cascade team missing: %d rows", teams)
	}
	if joins := f.count("SELECT count(*) FROM team_members WHERE team_id = $1", cascadeTeamID); joins != 3 {
		t.Fatalf("cascade membership missing: %d rows", joins)
	}

	status, _, body = f.admin(http.MethodDelete, "/api/admin/competitions/"+competitionID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete competition: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/competitions/"+competitionID)
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	if teams := f.count("SELECT count(*) FROM teams WHERE competition_id = $1", competitionID); teams != 0 {
		t.Fatalf("teams not cascaded: %d rows", teams)
	}
	if joins := f.count("SELECT count(*) FROM team_members WHERE competition_id = $1", competitionID); joins != 0 {
		t.Fatalf("team_members not cascaded: %d rows", joins)
	}
	if people := f.count("SELECT count(*) FROM people"); people != 3 {
		t.Fatalf("competition delete changed people: %d rows", people)
	}
}

func TestBackendIntegrationCompetitionRejectsYearZeroDate(t *testing.T) {
	f := newIntegrationFixture(t)
	competition := map[string]any{
		"name": "Year zero", "description": "desc", "date": "0000-06-01T10:00:00", "location": "Madrid",
	}
	status, _, body := f.admin(http.MethodPost, "/api/admin/competitions", competition)
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "date")

	competitionID := f.createCompetitionRow("Normal cup", 3)
	status, _, body = f.admin(http.MethodPatch, "/api/admin/competitions/"+competitionID.String(),
		map[string]any{"date": "0000-06-01T10:00:00"})
	integrationExpect(t, status, body, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, body, "date")
}

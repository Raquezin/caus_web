package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBackendIntegrationPublicRequests(t *testing.T) {
	f := newIntegrationFixture(t)
	published := f.createPublishedTalk("Public talk", true)
	draft := f.createPublishedTalk("Draft talk", false)
	competitionID := f.createCompetitionRow("Spring Cup", 3)
	teammate := f.insertUnregistered("Alan", "Turing")
	teamID := f.createTeamRow(competitionID, "Team Alpha", []uuid.UUID{f.memberID, teammate})
	f.exec("UPDATE teams SET position = 1 WHERE id = $1", teamID)

	for _, path := range []string{
		"/api/talks", "/api/talks/" + published.String(), "/api/talks.ics",
		"/api/competitions", "/api/competitions/" + competitionID.String(),
	} {
		status, _, body := f.public(http.MethodGet, path)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d body %s", path, status, body)
		}
		status, _, body = f.public(http.MethodHead, path)
		if status != http.StatusOK {
			t.Fatalf("HEAD %s: status %d body %s", path, status, body)
		}
	}

	status, header, body := f.public(http.MethodGet, "/api/auth/google/login")
	if status != http.StatusFound || !strings.Contains(header.Get("Location"), "accounts.example.test") {
		t.Fatalf("login: status %d location %q", status, header.Get("Location"))
	}
	status, _, body = f.public(http.MethodGet, "/api/auth/google/callback?state=forged&code=abc")
	integrationExpect(t, status, body, http.StatusBadRequest, "invalid_oauth_state")

	status, _, body = f.public(http.MethodGet, "/api/talks")
	if status != http.StatusOK {
		t.Fatalf("talks list: status %d body %s", status, body)
	}
	listed := map[string]bool{}
	for _, item := range integrationData(t, body) {
		listed[item.(map[string]any)["id"].(string)] = true
	}
	if !listed[published.String()] || listed[draft.String()] {
		t.Fatalf("talks list exposed %v", listed)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+published.String())
	if status != http.StatusOK {
		t.Fatalf("published talk: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+draft.String())
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	status, header, body = f.public(http.MethodGet, "/api/talks.ics")
	if status != http.StatusOK || !strings.HasPrefix(header.Get("Content-Type"), "text/calendar") {
		t.Fatalf("ics: status %d content-type %q", status, header.Get("Content-Type"))
	}
	feed := string(body)
	if !strings.Contains(feed, "Public talk") || strings.Contains(feed, "Draft talk") {
		t.Fatalf("ics feed %q", feed)
	}

	status, _, body = f.public(http.MethodGet, "/api/competitions")
	if status != http.StatusOK {
		t.Fatalf("competitions list: status %d body %s", status, body)
	}
	found := false
	for _, item := range integrationData(t, body) {
		if item.(map[string]any)["id"] == competitionID.String() {
			found = true
		}
	}
	if !found {
		t.Fatalf("competitions list missing %s: %s", competitionID, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/competitions/"+competitionID.String())
	if status != http.StatusOK {
		t.Fatalf("competition detail: status %d body %s", status, body)
	}
	detail := integrationJSON(t, body)
	teams, ok := detail["teams"].([]any)
	if !ok || len(teams) != 1 {
		t.Fatalf("competition teams %v", detail["teams"])
	}
	team := teams[0].(map[string]any)
	if team["name"] != "Team Alpha" || team["position"] != float64(1) {
		t.Fatalf("team result %v", team)
	}
	members, ok := team["members"].([]any)
	if !ok || len(members) != 2 {
		t.Fatalf("team members %v", team["members"])
	}
	for _, entry := range members {
		member := entry.(map[string]any)
		if len(member) != 3 || member["id"] == "" || member["first_name"] == "" || member["last_name"] == "" {
			t.Fatalf("member entry %v", member)
		}
		for _, private := range []string{"email", "google_sub", "role", "created_at", "updated_at"} {
			if _, ok := member[private]; ok {
				t.Fatalf("member entry exposed %q: %v", private, member)
			}
		}
	}

	unsupported := []struct{ method, path, allow string }{
		{http.MethodPut, "/api/talks", "GET, HEAD"},
		{http.MethodDelete, "/api/talks/" + published.String(), "GET, HEAD"},
		{http.MethodDelete, "/api/talks.ics", "GET, HEAD"},
		{http.MethodPost, "/api/competitions", "GET, HEAD"},
		{http.MethodPatch, "/api/competitions/" + competitionID.String(), "GET, HEAD"},
		{http.MethodPost, "/api/me", "DELETE, GET, HEAD, PATCH"},
		{http.MethodDelete, "/api/auth/logout", "POST"},
	}
	for _, testCase := range unsupported {
		status, header, body := f.public(testCase.method, testCase.path)
		integrationExpect(t, status, body, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := header.Get("Allow"); allow != testCase.allow {
			t.Fatalf("%s %s: Allow %q, want %q", testCase.method, testCase.path, allow, testCase.allow)
		}
		if strings.HasPrefix(header.Get("Content-Type"), "text/plain") {
			t.Fatalf("%s %s: plain text error body", testCase.method, testCase.path)
		}
	}
}

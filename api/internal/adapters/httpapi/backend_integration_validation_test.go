package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func integrationValidationField(t *testing.T, body []byte, field string) {
	t.Helper()
	errorObject, ok := integrationJSON(t, body)["error"].(map[string]any)
	if !ok {
		t.Fatalf("body %q has no error object", body)
	}
	fields, ok := errorObject["fields"].(map[string]any)
	if !ok || fields[field] == nil {
		t.Fatalf("body %q missing field error %q", body, field)
	}
}

func validIntegrationTalkBody(f *integrationFixture, title string) map[string]any {
	return map[string]any{
		"title": title, "description": "desc", "date": "2026-07-10T19:00:00",
		"duration": 3600, "location": "Madrid", "published": true,
		"speaker_ids": []string{f.memberID.String()},
	}
}

func TestBackendIntegrationRejectsInvalidTalkInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		field  string
	}{
		{"title NUL", func(b map[string]any) { b["title"] = "a\x00b" }, "title"},
		{"title control", func(b map[string]any) { b["title"] = "a\x01b" }, "title"},
		{"title DEL", func(b map[string]any) { b["title"] = "a\x7fb" }, "title"},
		{"description NUL", func(b map[string]any) { b["description"] = "a\x00b" }, "description"},
		{"description control", func(b map[string]any) { b["description"] = "a\x01b" }, "description"},
		{"description DEL", func(b map[string]any) { b["description"] = "a\x7fb" }, "description"},
		{"location NUL", func(b map[string]any) { b["location"] = "a\x00b" }, "location"},
		{"location control", func(b map[string]any) { b["location"] = "a\x01b" }, "location"},
		{"location DEL", func(b map[string]any) { b["location"] = "a\x7fb" }, "location"},
		{"huge duration", func(b map[string]any) { b["duration"] = int64(9223372037) }, "duration"},
		{"end overflow", func(b map[string]any) {
			b["date"] = "9999-12-31T23:30:00"
			b["duration"] = 7200
		}, "duration"},
		{"spring gap", func(b map[string]any) { b["date"] = "2026-03-29T02:30:00" }, "date"},
		{"year zero", func(b map[string]any) { b["date"] = "0000-06-01T12:00:00" }, "date"},
	}
	for _, tc := range cases {
		t.Run("create "+tc.name, func(t *testing.T) {
			f := newIntegrationFixture(t)
			body := validIntegrationTalkBody(f, "Unique "+tc.name)
			tc.mutate(body)
			status, _, response := f.admin(http.MethodPost, "/api/admin/talks", body)
			integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
			integrationValidationField(t, response, tc.field)
			status, _, list := f.admin(http.MethodGet, "/api/admin/talks", nil)
			integrationExpect(t, status, list, http.StatusOK, "")
			if data := integrationData(t, list); len(data) != 0 {
				t.Fatalf("invalid talk persisted: %s", list)
			}
			status, _, calendar := f.public(http.MethodGet, "/api/talks.ics")
			integrationExpect(t, status, calendar, http.StatusOK, "")
		})
		t.Run("patch "+tc.name, func(t *testing.T) {
			f := newIntegrationFixture(t)
			id := f.createPublishedTalk("Baseline "+tc.name, true).String()
			status, _, before := f.public(http.MethodGet, "/api/talks/"+id)
			integrationExpect(t, status, before, http.StatusOK, "")
			want := integrationJSON(t, before)
			body := map[string]any{}
			tc.mutate(body)
			status, _, response := f.admin(http.MethodPatch, "/api/admin/talks/"+id, body)
			integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
			integrationValidationField(t, response, tc.field)
			status, _, after := f.public(http.MethodGet, "/api/talks/"+id)
			integrationExpect(t, status, after, http.StatusOK, "")
			if got := integrationJSON(t, after); !reflect.DeepEqual(got, want) {
				t.Fatalf("rejected patch mutated talk: before %s after %s", before, after)
			}
			status, _, calendar := f.public(http.MethodGet, "/api/talks.ics")
			integrationExpect(t, status, calendar, http.StatusOK, "")
			if !strings.Contains(string(calendar), "Baseline "+tc.name) {
				t.Fatalf("baseline talk vanished from calendar: %s", calendar)
			}
		})
	}
}

func TestBackendIntegrationAcceptsMultilineUnicodeTalk(t *testing.T) {
	f := newIntegrationFixture(t)
	body := validIntegrationTalkBody(f, "Señales λ\nsegunda línea")
	body["description"] = "primera línea\r\nsegunda línea"
	body["location"] = "Sala Ω"
	status, _, response := f.admin(http.MethodPost, "/api/admin/talks", body)
	integrationExpect(t, status, response, http.StatusCreated, "")
	status, _, calendar := f.public(http.MethodGet, "/api/talks.ics")
	integrationExpect(t, status, calendar, http.StatusOK, "")
	lines := calendarLogicalLines(t, string(calendar))
	for prefix, want := range map[string]string{
		"SUMMARY:":     `SUMMARY:Señales λ\nsegunda línea`,
		"DESCRIPTION:": `DESCRIPTION:primera línea\nsegunda línea`,
		"LOCATION:":    `LOCATION:Sala Ω`,
	} {
		values := calendarValues(lines, prefix)
		if len(values) != 1 || values[0] != want {
			t.Fatalf("calendar %q values %v, want %q", prefix, values, want)
		}
	}
}

func TestBackendIntegrationRejectsInvalidCompetitionAndTeamText(t *testing.T) {
	f := newIntegrationFixture(t)
	competition := map[string]any{
		"name": "Review Cup", "description": "desc", "date": "2026-09-15T10:00:00",
		"location": "Madrid", "team_size": 3,
	}
	for _, field := range []string{"name", "description", "location"} {
		bad := map[string]any{}
		for key, value := range competition {
			bad[key] = value
		}
		bad[field] = "a\x00b"
		status, _, response := f.admin(http.MethodPost, "/api/admin/competitions", bad)
		integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
		integrationValidationField(t, response, field)
	}
	status, header, response := f.admin(http.MethodPost, "/api/admin/competitions", competition)
	integrationExpect(t, status, response, http.StatusCreated, "")
	competitionID := strings.TrimPrefix(header.Get("Location"), "/api/admin/competitions/")
	if competitionID == "" || competitionID == header.Get("Location") {
		t.Fatalf("no competition location: %v", header)
	}
	status, _, before := f.admin(http.MethodGet, "/api/admin/competitions/"+competitionID, nil)
	integrationExpect(t, status, before, http.StatusOK, "")
	want := integrationJSON(t, before)
	for _, field := range []string{"name", "description", "location"} {
		status, _, response = f.admin(http.MethodPatch, "/api/admin/competitions/"+competitionID,
			map[string]any{field: "a\x00b"})
		integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
		integrationValidationField(t, response, field)
	}
	status, _, after := f.admin(http.MethodGet, "/api/admin/competitions/"+competitionID, nil)
	integrationExpect(t, status, after, http.StatusOK, "")
	if got := integrationJSON(t, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected competition patches mutated resource: before %s after %s", before, after)
	}
	status, _, response = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", map[string]any{
		"name": "a\x00b", "member_ids": []string{f.memberID.String()},
	})
	integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
	integrationValidationField(t, response, "name")
	status, header, response = f.admin(http.MethodPost, "/api/admin/competitions/"+competitionID+"/teams", map[string]any{
		"name": "Valid Team", "member_ids": []string{f.memberID.String()},
	})
	integrationExpect(t, status, response, http.StatusCreated, "")
	teamID := strings.TrimPrefix(header.Get("Location"), "/api/admin/teams/")
	if teamID == "" || teamID == header.Get("Location") {
		t.Fatalf("no team location: %v", header)
	}
	status, _, beforeTeam := f.admin(http.MethodGet, "/api/admin/teams/"+teamID, nil)
	integrationExpect(t, status, beforeTeam, http.StatusOK, "")
	wantTeam := integrationJSON(t, beforeTeam)
	for _, value := range []string{"a\x00b", "a\x7fb"} {
		status, _, response = f.admin(http.MethodPatch, "/api/admin/teams/"+teamID, map[string]any{"name": value})
		integrationExpect(t, status, response, http.StatusUnprocessableEntity, "validation_failed")
		integrationValidationField(t, response, "name")
	}
	status, _, afterTeam := f.admin(http.MethodGet, "/api/admin/teams/"+teamID, nil)
	integrationExpect(t, status, afterTeam, http.StatusOK, "")
	if got := integrationJSON(t, afterTeam); !reflect.DeepEqual(got, wantTeam) {
		t.Fatalf("rejected team patches mutated resource: before %s after %s", beforeTeam, afterTeam)
	}
}

func TestBackendRateLimitProxiedClientBuckets(t *testing.T) {
	f := newIntegrationFixture(t)
	for i := 0; i < 60; i++ {
		status, _, body := f.do(integrationRequest{
			method:  http.MethodPost,
			path:    "/api/talks",
			headers: map[string]string{"X-Forwarded-For": "198.51.100.1"},
		})
		if status != http.StatusMethodNotAllowed {
			t.Fatalf("request %d: status %d body %s", i+1, status, body)
		}
	}
	status, _, body := f.do(integrationRequest{
		method:  http.MethodGet,
		path:    "/api/auth/google/login",
		headers: map[string]string{"X-Forwarded-For": "198.51.100.2"},
	})
	if status != http.StatusFound {
		t.Fatalf("distinct proxied client: status %d body %s", status, body)
	}
	status, _, body = f.do(integrationRequest{
		method:  http.MethodPost,
		path:    "/api/talks",
		headers: map[string]string{"X-Forwarded-For": "198.51.100.1"},
	})
	integrationExpect(t, status, body, http.StatusTooManyRequests, "rate_limited")
}

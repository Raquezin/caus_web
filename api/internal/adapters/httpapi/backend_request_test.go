package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func decodeDTO(t *testing.T, body string, target any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	if !decodeBody(recorder, request, target) {
		t.Fatalf("decode %s: %d %s", body, recorder.Code, recorder.Body)
	}
}

const talkCreateBody = `{"title":"Talk","description":"Desc","date":"2026-03-14T09:30:00",` +
	`"duration":3600,"location":"Room","speaker_ids":["11111111-1111-1111-1111-111111111111"]}`

func TestTalkRequestDefaults(t *testing.T) {
	var request talkRequest
	decodeDTO(t, talkCreateBody, &request)
	input, err := request.input()
	if err != nil {
		t.Fatal(err)
	}
	if input.Language != "es" || input.Published || input.StreamURL != nil || input.PosterURL != nil {
		t.Fatalf("defaults %+v", input)
	}
	if input.Duration != 3600 || !input.Date.Equal(time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("converted values %+v", input)
	}
	var explicit talkRequest
	decodeDTO(t, `{"title":"Talk","description":"Desc","date":"2026-03-14T09:30:00","duration":1,`+
		`"location":"Room","language":"en","published":true,"stream_url":"https://example.com/s",`+
		`"poster_url":null,"speaker_ids":["11111111-1111-1111-1111-111111111111"]}`, &explicit)
	input, err = explicit.input()
	if err != nil || input.Language != "en" || !input.Published || input.StreamURL == nil || input.PosterURL != nil {
		t.Fatalf("explicit values %+v %v", input, err)
	}
}

func TestTalkRequestRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"null language":    strings.Replace(talkCreateBody, `"title":"Talk"`, `"title":"Talk","language":null`, 1),
		"null published":   strings.Replace(talkCreateBody, `"title":"Talk"`, `"title":"Talk","published":null`, 1),
		"missing title":    strings.Replace(talkCreateBody, `"title":"Talk",`, ``, 1),
		"null title":       strings.Replace(talkCreateBody, `"title":"Talk"`, `"title":null`, 1),
		"missing speakers": strings.Replace(talkCreateBody, `,"speaker_ids":["11111111-1111-1111-1111-111111111111"]`, ``, 1),
		"offset date":      strings.Replace(talkCreateBody, `2026-03-14T09:30:00`, `2026-03-14T09:30:00+01:00`, 1),
		"fractional":       strings.Replace(talkCreateBody, `"duration":3600`, `"duration":3600.5`, 1),
		"text duration":    strings.Replace(talkCreateBody, `"duration":3600`, `"duration":"3600"`, 1),
		"invalid speaker":  strings.Replace(talkCreateBody, `11111111-1111-1111-1111-111111111111`, `nobody`, 1),
		"null speakers":    strings.Replace(talkCreateBody, `["11111111-1111-1111-1111-111111111111"]`, `null`, 1),
	}
	for name, body := range cases {
		var request talkRequest
		decodeDTO(t, body, &request)
		if _, err := request.input(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(talkCreateBody+`{"id":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	if decodeBody(recorder, request, &talkRequest{}) || recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status %d", recorder.Code)
	}
}

func TestTalkPatchRequest(t *testing.T) {
	var request talkPatchRequest
	decodeDTO(t, `{"title":"Updated","published":false,"stream_url":null,"poster_url":"https://example.com/p",`+
		`"speaker_ids":[]}`, &request)
	patch, err := request.patch()
	if err != nil {
		t.Fatal(err)
	}
	if patch.Title == nil || *patch.Title != "Updated" || patch.Published == nil || *patch.Published ||
		patch.StreamURL == nil || patch.StreamURL.Value != nil || patch.PosterURL == nil ||
		patch.PosterURL.Value == nil || patch.SpeakerIDs == nil || len(*patch.SpeakerIDs) != 0 {
		t.Fatalf("patch %+v", patch)
	}
	if patch.Description != nil || patch.Date != nil || patch.Duration != nil {
		t.Fatalf("omitted fields set %+v", patch)
	}
	var empty talkPatchRequest
	decodeDTO(t, `{}`, &empty)
	patch, err = empty.patch()
	if err != nil {
		t.Fatal(err)
	}
	if patch.Title != nil || patch.StreamURL != nil || patch.SpeakerIDs != nil {
		t.Fatalf("empty patch %+v", patch)
	}
	var falseRequest talkPatchRequest
	decodeDTO(t, `{"published":false}`, &falseRequest)
	if patch, err := falseRequest.patch(); err != nil || patch.Published == nil || *patch.Published {
		t.Fatalf("false published %+v %v", patch, err)
	}
	for name, body := range map[string]string{
		"null title":     `{"title":null}`,
		"null duration":  `{"duration":null}`,
		"null published": `{"published":null}`,
		"null speakers":  `{"speaker_ids":null}`,
		"bad date":       `{"date":"2026-03-29T02:30:00"}`,
	} {
		var request talkPatchRequest
		decodeDTO(t, body, &request)
		if _, err := request.patch(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestCompetitionRequestAndPatch(t *testing.T) {
	var request competitionRequest
	decodeDTO(t, `{"name":"Spring","description":"Desc","date":"2026-05-02T18:00:00","location":"Hall"}`, &request)
	input, err := request.input()
	if err != nil {
		t.Fatal(err)
	}
	if input.TeamSize != 3 {
		t.Fatalf("default team size %d", input.TeamSize)
	}
	var explicit competitionRequest
	decodeDTO(t, `{"name":"Spring","description":"Desc","date":"2026-05-02T18:00:00","location":"Hall","team_size":5}`, &explicit)
	if input, err := explicit.input(); err != nil || input.TeamSize != 5 {
		t.Fatalf("explicit team size %+v %v", input, err)
	}
	var null competitionRequest
	decodeDTO(t, `{"name":"Spring","description":"Desc","date":"2026-05-02T18:00:00","location":"Hall","team_size":null}`, &null)
	if _, err := null.input(); err == nil {
		t.Fatal("null team size accepted")
	}
	var patchRequest competitionPatchRequest
	decodeDTO(t, `{"name":"Autumn","team_size":4}`, &patchRequest)
	patch, err := patchRequest.patch()
	if err != nil || patch.Name == nil || patch.TeamSize == nil || *patch.TeamSize != 4 {
		t.Fatalf("competition patch %+v %v", patch, err)
	}
	var nullPatch competitionPatchRequest
	decodeDTO(t, `{"team_size":null}`, &nullPatch)
	if _, err := nullPatch.patch(); err == nil {
		t.Fatal("null team size patch accepted")
	}
}

func TestTeamRequestAndPatch(t *testing.T) {
	var request teamRequest
	decodeDTO(t, `{"member_ids":["11111111-1111-1111-1111-111111111111"]}`, &request)
	input, err := request.input()
	if err != nil {
		t.Fatal(err)
	}
	if input.Name != nil || input.PhotoURL != nil || input.Position != nil || len(input.MemberIDs) != 1 {
		t.Fatalf("team input %+v", input)
	}
	var explicit teamRequest
	decodeDTO(t, `{"name":"Team A","photo_url":"https://example.com/p.png","position":2,`+
		`"member_ids":["11111111-1111-1111-1111-111111111111"]}`, &explicit)
	input, err = explicit.input()
	if err != nil || input.Name == nil || *input.Name != "Team A" || input.Position == nil || *input.Position != 2 {
		t.Fatalf("explicit team input %+v %v", input, err)
	}
	var null teamRequest
	decodeDTO(t, `{"name":null,"photo_url":null,"position":null,`+
		`"member_ids":["11111111-1111-1111-1111-111111111111"]}`, &null)
	input, err = null.input()
	if err != nil || input.Name != nil || input.PhotoURL != nil || input.Position != nil {
		t.Fatalf("null team input %+v %v", input, err)
	}
	var patchRequest teamPatchRequest
	decodeDTO(t, `{"name":null,"position":null,"member_ids":["11111111-1111-1111-1111-111111111111"]}`, &patchRequest)
	patch, err := patchRequest.patch()
	if err != nil || patch.Name == nil || patch.Name.Value != nil || patch.Position == nil ||
		patch.Position.Value != nil || patch.MemberIDs == nil || patch.PhotoURL != nil {
		t.Fatalf("team patch %+v %v", patch, err)
	}
	var nullMembers teamPatchRequest
	decodeDTO(t, `{"member_ids":null}`, &nullMembers)
	if _, err := nullMembers.patch(); err == nil {
		t.Fatal("null members patch accepted")
	}
	var missing teamRequest
	decodeDTO(t, `{}`, &missing)
	if _, err := missing.input(); err == nil {
		t.Fatal("missing member_ids accepted")
	}
}

func TestDomainStructsHaveNoJSONTags(t *testing.T) {
	encoded, err := json.Marshal(domain.Team{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"Members"`) || strings.Contains(text, `"photo_url"`) || strings.Contains(text, `"competition_id"`) {
		t.Fatalf("domain team JSON %s", text)
	}
}

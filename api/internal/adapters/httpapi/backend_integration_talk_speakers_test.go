package httpapi

import (
	"net/http"
	"testing"
)

func TestBackendIntegrationTalkKeepsSpeakerWithDeletedAccount(t *testing.T) {
	f := newIntegrationFixture(t)
	three := f.insertPerson("Tres", "Persona", "tres@example.test", "sub-tres", "member")
	status, _, body := f.admin(http.MethodPost, "/api/admin/talks", map[string]any{
		"title":       "Historic",
		"description": "charla",
		"date":        "2026-07-10T19:00:00",
		"duration":    3600,
		"location":    "Madrid",
		"language":    "es",
		"speaker_ids": []string{three.String()},
	})
	if status != http.StatusCreated {
		t.Fatalf("create talk: status %d body %s", status, body)
	}
	talkID, _ := integrationJSON(t, body)["id"].(string)
	if talkID == "" {
		t.Fatalf("create talk body %s", body)
	}

	status, _, body = f.do(integrationRequest{
		method: http.MethodDelete, path: "/api/me", token: f.createSession(three), origins: []string{f.origin},
	})
	if status != http.StatusNoContent {
		t.Fatalf("delete speaker account: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+talkID,
		map[string]any{"speaker_ids": []string{three.String()}})
	if status != http.StatusOK {
		t.Fatalf("keep deleted speaker: status %d body %s", status, body)
	}
	if speakers, ok := integrationJSON(t, body)["speakers"].([]any); !ok || len(speakers) != 1 {
		t.Fatalf("kept speakers %v", integrationJSON(t, body)["speakers"])
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+talkID,
		map[string]any{"speaker_ids": []string{three.String(), f.memberID.String()}})
	if status != http.StatusOK {
		t.Fatalf("add registered co-speaker: status %d body %s", status, body)
	}
	if speakers, ok := integrationJSON(t, body)["speakers"].([]any); !ok || len(speakers) != 2 {
		t.Fatalf("speakers after adding a co-speaker %v", integrationJSON(t, body)["speakers"])
	}
}

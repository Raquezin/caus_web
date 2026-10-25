package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestBackendIntegrationTalkLifecycle(t *testing.T) {
	f := newIntegrationFixture(t)
	create := map[string]any{
		"title":       "Rust en produccion",
		"description": "charla",
		"date":        "2026-07-10T19:00:00",
		"duration":    3600,
		"location":    "Madrid",
		"language":    "en",
		"stream_url":  "https://example.com/live",
		"poster_url":  "https://example.com/poster.png",
		"speaker_ids": []string{f.memberID.String(), f.adminID.String()},
	}
	status, header, body := f.admin(http.MethodPost, "/api/admin/talks", create)
	if status != http.StatusCreated {
		t.Fatalf("create: status %d body %s", status, body)
	}
	created := integrationJSON(t, body)
	id, _ := created["id"].(string)
	if id == "" || header.Get("Location") != "/api/admin/talks/"+id {
		t.Fatalf("create: id %q location %q", id, header.Get("Location"))
	}
	if created["published"] != false || created["stream_url"] != "https://example.com/live" {
		t.Fatalf("create body %v", created)
	}
	if speakers, ok := created["speakers"].([]any); !ok || len(speakers) != 2 {
		t.Fatalf("create speakers %v", created["speakers"])
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/talks/"+id, nil)
	if status != http.StatusOK {
		t.Fatalf("admin get: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+id)
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")

	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+id, map[string]any{
		"published": true, "stream_url": nil,
	})
	if status != http.StatusOK {
		t.Fatalf("patch publish: status %d body %s", status, body)
	}
	published := integrationJSON(t, body)
	if published["published"] != true || published["stream_url"] != nil ||
		published["poster_url"] != "https://example.com/poster.png" {
		t.Fatalf("patch publish body %v", published)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks")
	if !strings.Contains(string(body), id) {
		t.Fatalf("public list missing published talk: %s", body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+id)
	if status != http.StatusOK {
		t.Fatalf("public detail: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks.ics")
	if status != http.StatusOK || !strings.Contains(string(body), "Rust en produccion") {
		t.Fatalf("ics: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+id, map[string]any{"published": false})
	if status != http.StatusOK || integrationJSON(t, body)["published"] != false {
		t.Fatalf("patch unpublish: status %d body %s", status, body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks")
	if strings.Contains(string(body), id) {
		t.Fatalf("public list exposed unpublished talk: %s", body)
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+id)
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	status, _, body = f.public(http.MethodGet, "/api/talks.ics")
	if strings.Contains(string(body), "Rust en produccion") {
		t.Fatalf("ics exposed unpublished talk: %s", body)
	}

	status, _, body = f.admin(http.MethodPatch, "/api/admin/talks/"+id, map[string]any{
		"published":   true,
		"speaker_ids": []string{f.adminID.String()},
	})
	if status != http.StatusOK {
		t.Fatalf("patch speakers: status %d body %s", status, body)
	}
	updated := integrationJSON(t, body)
	speakers, ok := updated["speakers"].([]any)
	if !ok || len(speakers) != 1 || speakers[0].(map[string]any)["id"] != f.adminID.String() {
		t.Fatalf("patch speakers %v", updated["speakers"])
	}
	status, _, body = f.public(http.MethodGet, "/api/talks/"+id)
	if status != http.StatusOK {
		t.Fatalf("public detail after republish: status %d body %s", status, body)
	}

	status, _, body = f.admin(http.MethodDelete, "/api/admin/talks/"+id, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: status %d body %s", status, body)
	}
	status, _, body = f.admin(http.MethodGet, "/api/admin/talks/"+id, nil)
	integrationExpect(t, status, body, http.StatusNotFound, "not_found")
	for _, personID := range []string{f.memberID.String(), f.adminID.String()} {
		if people := f.count("SELECT count(*) FROM people WHERE id = $1", personID); people != 1 {
			t.Fatalf("speaker %s removed with the talk", personID)
		}
	}
	if links := f.count("SELECT count(*) FROM talk_speakers"); links != 0 {
		t.Fatalf("talk_speakers not cascaded: %d rows", links)
	}
}

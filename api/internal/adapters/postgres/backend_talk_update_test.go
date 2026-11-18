package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertEndpointTalkUpdatePerson(t *testing.T, tx pgx.Tx, registered bool) uuid.UUID {
	t.Helper()
	suffix := uuid.NewString()
	var id uuid.UUID
	var err error
	if registered {
		err = tx.QueryRow(context.Background(),
			"INSERT INTO people (first_name, last_name, email, google_sub) VALUES ('Ada', 'Lovelace', $1, $2) RETURNING id",
			"speaker-"+suffix+"@example.com", "sub-"+suffix).Scan(&id)
	} else {
		err = tx.QueryRow(context.Background(),
			"INSERT INTO people (first_name, last_name) VALUES ('Historic', 'Speaker') RETURNING id").Scan(&id)
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertEndpointTalkUpdateTalk(t *testing.T, tx pgx.Tx, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO talks (title, description, date, duration, location, language, stream_url, poster_url, published) "+
			"VALUES ($1, 'Description', '2026-03-14 09:30:00', interval '1 hour 30 minutes', 'Room 1', 'en', "+
			"'https://example.com/stream', 'https://example.com/poster', true) RETURNING id", title).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func attachEndpointTalkUpdateSpeakers(t *testing.T, tx pgx.Tx, talkID uuid.UUID, ids ...uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, id); err != nil {
			t.Fatal(err)
		}
	}
}

func endpointTalkUpdateSpeakerSet(t *testing.T, talk domain.Talk) map[uuid.UUID]bool {
	t.Helper()
	ids := make(map[uuid.UUID]bool, len(talk.Speakers))
	for _, speaker := range talk.Speakers {
		ids[speaker.ID] = true
	}
	return ids
}

func TestEndpointTalkUpdatePreservesOmittedFields(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	registered := insertEndpointTalkUpdatePerson(t, tx, true)
	historical := insertEndpointTalkUpdatePerson(t, tx, false)
	talkID := insertEndpointTalkUpdateTalk(t, tx, "Original title "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, registered, historical)
	before, err := readTalk(ctx, tx, talkID, true)
	if err != nil {
		t.Fatal(err)
	}

	title := "Renamed title"
	published := false
	updated, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{
		Title:     &title,
		Published: &published,
		StreamURL: &domain.NullableText{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != title || updated.Published || updated.StreamURL != nil {
		t.Fatalf("updated scalar fields %+v", updated)
	}
	if updated.Description != before.Description || updated.Location != before.Location ||
		updated.Language != before.Language || updated.Duration != before.Duration || !updated.Date.Equal(before.Date) {
		t.Fatalf("omitted fields changed: %+v", updated)
	}
	if updated.PosterURL == nil || before.PosterURL == nil || *updated.PosterURL != *before.PosterURL {
		t.Fatalf("omitted poster url changed: %+v", updated.PosterURL)
	}
	speakers := endpointTalkUpdateSpeakerSet(t, updated)
	if len(speakers) != 2 || !speakers[registered] || !speakers[historical] {
		t.Fatalf("speakers changed while omitted: %+v", updated.Speakers)
	}

	persisted, err := readTalk(ctx, tx, talkID, true)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Title != title || persisted.Published || persisted.StreamURL != nil {
		t.Fatalf("persisted scalar fields %+v", persisted)
	}
}

func TestEndpointTalkUpdateReplacesSpeakers(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	original := insertEndpointTalkUpdatePerson(t, tx, true)
	first := insertEndpointTalkUpdatePerson(t, tx, true)
	second := insertEndpointTalkUpdatePerson(t, tx, true)
	talkID := insertEndpointTalkUpdateTalk(t, tx, "Speaker talk "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, original)

	requested := []uuid.UUID{first, second}
	updated, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{SpeakerIDs: &requested})
	if err != nil {
		t.Fatal(err)
	}
	speakers := endpointTalkUpdateSpeakerSet(t, updated)
	if len(speakers) != 2 || !speakers[first] || !speakers[second] || speakers[original] {
		t.Fatalf("replaced speakers %+v", updated.Speakers)
	}
	var stale int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talk_speakers WHERE talk_id = $1 AND person_id = $2",
		talkID, original).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatal("previous speaker membership survived replacement")
	}
}

func TestEndpointTalkUpdateRejectsSpeakers(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	original := insertEndpointTalkUpdatePerson(t, tx, true)
	unregistered := insertEndpointTalkUpdatePerson(t, tx, false)
	historical := insertEndpointTalkUpdatePerson(t, tx, false)
	talkID := insertEndpointTalkUpdateTalk(t, tx, "Reject talk "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, original, historical)

	cases := map[string][]uuid.UUID{
		"empty":        {},
		"unknown":      {uuid.New()},
		"unregistered": {unregistered},
	}
	for name, ids := range cases {
		requested := ids
		_, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{SpeakerIDs: &requested})
		var validation domain.ValidationError
		if !errors.As(err, &validation) || validation.Fields["speaker_ids"] == "" {
			t.Fatalf("%s speakers error %v", name, err)
		}
	}
	persisted, err := readTalk(ctx, tx, talkID, true)
	if err != nil {
		t.Fatal(err)
	}
	speakers := endpointTalkUpdateSpeakerSet(t, persisted)
	if len(speakers) != 2 || !speakers[original] || !speakers[historical] {
		t.Fatalf("rejected patch changed speakers: %+v", persisted.Speakers)
	}
}

func TestEndpointTalkUpdateRejectsEmptyPatch(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertEndpointTalkUpdatePerson(t, tx, true)
	title := "Empty patch " + uuid.NewString()
	talkID := insertEndpointTalkUpdateTalk(t, tx, title)
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, speaker)

	_, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{})
	var validation domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["patch"] == "" {
		t.Fatalf("empty patch error %v", err)
	}
	assertEndpointTalkUpdateTitle(t, tx, talkID, title)
}

func TestEndpointTalkUpdateRollsBackScalars(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertEndpointTalkUpdatePerson(t, tx, true)
	title := "Stable title " + uuid.NewString()
	talkID := insertEndpointTalkUpdateTalk(t, tx, title)
	attachEndpointTalkUpdateSpeakers(t, tx, talkID, speaker)

	changed := "Changed title"
	unknown := []uuid.UUID{uuid.New()}
	if _, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{Title: &changed, SpeakerIDs: &unknown}); err == nil {
		t.Fatal("unknown speaker accepted")
	}
	assertEndpointTalkUpdateTitle(t, tx, talkID, title)

	duplicate := insertEndpointTalkUpdateTalk(t, tx, "Duplicate title "+uuid.NewString())
	attachEndpointTalkUpdateSpeakers(t, tx, duplicate, speaker)
	duplicateTitle := "Duplicate title"
	if _, err := repo.UpdateTalk(ctx, duplicate, domain.TalkPatch{Title: &duplicateTitle}); err != nil {
		t.Fatalf("prepare duplicate: %v", err)
	}
	if _, err := repo.UpdateTalk(ctx, talkID, domain.TalkPatch{Title: &duplicateTitle}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate title error %v", err)
	}
	assertEndpointTalkUpdateTitle(t, tx, talkID, title)
}

func assertEndpointTalkUpdateTitle(t *testing.T, tx pgx.Tx, id uuid.UUID, want string) {
	t.Helper()
	var title string
	if err := tx.QueryRow(context.Background(), "SELECT title FROM talks WHERE id = $1", id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != want {
		t.Fatalf("talk title %q, want %q", title, want)
	}
}

func TestEndpointTalkUpdateMissingTalk(t *testing.T) {
	repo, _ := backendFixture(t)
	title := "Missing"
	_, err := repo.UpdateTalk(context.Background(), uuid.New(), domain.TalkPatch{Title: &title})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing talk error %v", err)
	}
}

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTalkGetRepositoryIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	speakerID := insertPersonRow(t, tx, "Ada", "Lovelace")
	talkID := insertTalkRow(t, tx, true)
	if _, err := tx.Exec(ctx,
		"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, speakerID); err != nil {
		t.Fatal(err)
	}
	talk, err := repo.GetTalk(ctx, talkID, false)
	if err != nil {
		t.Fatalf("public talk: %v", err)
	}
	if talk.ID != talkID || talk.Title == "" || talk.Description != "Description" || talk.Location != "Room 1" ||
		talk.Language != "en" || talk.Duration != 5400 || !talk.Published || talk.StreamURL == nil || talk.PosterURL == nil {
		t.Fatalf("unexpected published talk %+v", talk)
	}
	wantDate := time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)
	if !talk.Date.Equal(wantDate) || talk.Date.Location() != time.UTC {
		t.Fatalf("date %v, want %v", talk.Date, wantDate)
	}
	if len(talk.Speakers) != 1 || talk.Speakers[0].ID != speakerID ||
		talk.Speakers[0].FirstName != "Ada" || talk.Speakers[0].LastName != "Lovelace" {
		t.Fatalf("speakers %+v", talk.Speakers)
	}

	adminSpeakerID := insertPersonRow(t, tx, "Grace", "Hopper")
	if _, err := tx.Exec(ctx,
		"UPDATE people SET email = 'grace@example.com', google_sub = 'sub-grace', role = 'admin' WHERE id = $1", adminSpeakerID); err != nil {
		t.Fatal(err)
	}
	hiddenID := insertTalkRow(t, tx, false)
	if _, err := tx.Exec(ctx,
		"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", hiddenID, adminSpeakerID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTalk(ctx, hiddenID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unpublished talk read as public: %v", err)
	}
	hidden, err := repo.GetTalk(ctx, hiddenID, true)
	if err != nil {
		t.Fatalf("admin read of unpublished talk: %v", err)
	}
	if hidden.ID != hiddenID || hidden.Published {
		t.Fatalf("unexpected unpublished talk %+v", hidden)
	}
	if len(hidden.Speakers) != 1 || hidden.Speakers[0].ID != adminSpeakerID ||
		hidden.Speakers[0].FirstName != "Grace" || hidden.Speakers[0].LastName != "Hopper" {
		t.Fatalf("unpublished speakers %+v", hidden.Speakers)
	}

	for _, includeUnpublished := range []bool{false, true} {
		if _, err := repo.GetTalk(ctx, uuid.New(), includeUnpublished); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing talk includeUnpublished=%v: %v", includeUnpublished, err)
		}
		if _, err := repo.GetTalk(ctx, uuid.Nil, includeUnpublished); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("nil talk includeUnpublished=%v: %v", includeUnpublished, err)
		}
	}
}

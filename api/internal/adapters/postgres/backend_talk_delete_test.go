package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTalkDeleteIntegration(t *testing.T) {
	repository, tx := backendFixture(t)
	ctx := context.Background()
	targetSpeakerA := insertPersonRow(t, tx, "Ada", "Lovelace")
	targetSpeakerB := insertPersonRow(t, tx, "Grace", "Hopper")
	otherSpeaker := insertPersonRow(t, tx, "Alan", "Turing")
	targetID := insertTalkRow(t, tx, true)
	otherID := insertTalkRow(t, tx, false)
	for _, speakerID := range []uuid.UUID{targetSpeakerA, targetSpeakerB} {
		if _, err := tx.Exec(ctx,
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", targetID, speakerID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", otherID, otherSpeaker); err != nil {
		t.Fatal(err)
	}
	before, err := readTalk(ctx, tx, otherID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Speakers) != 1 || before.Speakers[0].ID != otherSpeaker {
		t.Fatalf("other talk speakers %+v", before.Speakers)
	}

	if err := repository.DeleteTalk(ctx, targetID); err != nil {
		t.Fatalf("delete talk: %v", err)
	}
	if _, err := readTalk(ctx, tx, targetID, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted talk read: %v", err)
	}
	var joins int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talk_speakers WHERE talk_id = $1", targetID).Scan(&joins); err != nil {
		t.Fatal(err)
	}
	if joins != 0 {
		t.Fatalf("deleted talk speakers remain: %d", joins)
	}
	var people int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM people WHERE id IN ($1, $2, $3)",
		targetSpeakerA, targetSpeakerB, otherSpeaker).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 3 {
		t.Fatalf("speakers deleted with the talk: %d people", people)
	}

	after, err := readTalk(ctx, tx, otherID, true)
	if err != nil {
		t.Fatalf("other talk after delete: %v", err)
	}
	if after.Title != before.Title || after.Date != before.Date || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("other talk changed: %+v -> %+v", before, after)
	}
	if len(after.Speakers) != 1 || after.Speakers[0].ID != otherSpeaker {
		t.Fatalf("other talk speakers changed: %+v", after.Speakers)
	}
	var otherJoins int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talk_speakers WHERE talk_id = $1", otherID).Scan(&otherJoins); err != nil {
		t.Fatal(err)
	}
	if otherJoins != 1 {
		t.Fatalf("other talk joins %d", otherJoins)
	}

	for _, id := range []uuid.UUID{targetID, uuid.New()} {
		if err := repository.DeleteTalk(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delete %v: %v, want not found", id, err)
		}
	}
}

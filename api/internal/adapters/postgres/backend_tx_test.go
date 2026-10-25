package postgres

import (
	"context"
	"errors"
	"testing"

	"caus-platform/api/internal/domain"
)

func TestFinishBackendTxIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	nested, err := repo.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	speakerID := insertPersonRow(t, nested, "Ada", "Lovelace")
	talkID := insertTalkRow(t, nested, false)
	if _, err := nested.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, speakerID); err != nil {
		t.Fatal(err)
	}
	if err := finishBackendTx(ctx, nested); err != nil {
		t.Fatalf("commit nested transaction: %v", err)
	}
	persisted, err := readTalk(ctx, tx, talkID, true)
	if err != nil || persisted.ID != talkID {
		t.Fatalf("nested write not visible: %v", err)
	}
	failing, err := repo.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	insertTalkRow(t, failing, true)
	err = finishBackendTx(ctx, failing)
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("deferred constraint error %v, want validation error", err)
	}
	_ = failing.Rollback(ctx)
	var talks int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talks").Scan(&talks); err != nil {
		t.Fatal(err)
	}
	if talks != 1 {
		t.Fatalf("failed nested transaction leaked rows: %d talks", talks)
	}
}

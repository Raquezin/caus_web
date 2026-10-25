package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"caus-platform/api/internal/domain"
)

func installTalkSpeakersInsertTrigger(t *testing.T, tx pgx.Tx, name, body string) {
	t.Helper()
	ctx := context.Background()
	if _, err := tx.Exec(ctx,
		"create function "+name+"() returns trigger language plpgsql as $$begin "+body+" end;$$"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx,
		"create trigger "+name+" before insert on talk_speakers for each row execute function "+name+"()"); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointTalkCreateRollsBackSpeakerForeignKey(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertRegisteredSpeaker(t, tx, "Ada", "Lovelace", "ada@example.com")
	installTalkSpeakersInsertTrigger(t, tx, "talk_speakers_force_missing_person",
		"new.person_id := gen_random_uuid(); return new;")

	subTx, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := registeredTalkInput(speaker)
	var talkID uuid.UUID
	err = subTx.QueryRow(ctx,
		"INSERT INTO talks (title, description, date, duration, location, language, stream_url, poster_url, published) "+
			"VALUES ($1, $2, $3, make_interval(secs => $4::double precision), $5, $6, $7, $8, $9) RETURNING id",
		input.Title, input.Description, naiveTimestamp(input.Date), input.Duration, input.Location, input.Language,
		input.StreamURL, input.PosterURL, input.Published).Scan(&talkID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = subTx.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, speaker)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("speaker insert error %v, want sqlstate 23503", err)
	}
	_ = subTx.Rollback(ctx)

	_, err = repo.CreateTalk(ctx, registeredTalkInput(speaker))
	var validation domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["data"] == "" {
		t.Fatalf("create talk error %v, want data validation error", err)
	}
	if count := countTalkRows(t, tx); count != 0 {
		t.Fatalf("rolled back create leaked %d talks", count)
	}
	var speakerRows int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talk_speakers").Scan(&speakerRows); err != nil {
		t.Fatal(err)
	}
	if speakerRows != 0 {
		t.Fatalf("rolled back create leaked %d talk_speakers", speakerRows)
	}

	grace := "grace@example.com"
	insertRegisteredSpeaker(t, tx, "Grace", "Hopper", grace)
	var people int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM people WHERE email = $1", grace).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 1 {
		t.Fatalf("fixture transaction unusable: %d people for email", people)
	}
}

func TestEndpointTalkCreateRollsBackMissingSpeakerConstraint(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertRegisteredSpeaker(t, tx, "Alan", "Turing", "alan@example.com")
	installTalkSpeakersInsertTrigger(t, tx, "talk_speakers_suppress_insert", "return null;")

	_, err := repo.CreateTalk(ctx, registeredTalkInput(speaker))
	var validation domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["data"] == "" {
		t.Fatalf("create talk error %v, want data validation error", err)
	}
	if count := countTalkRows(t, tx); count != 0 {
		t.Fatalf("rolled back create leaked %d talks", count)
	}
	var speakerRows int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM talk_speakers").Scan(&speakerRows); err != nil {
		t.Fatal(err)
	}
	if speakerRows != 0 {
		t.Fatalf("rolled back create leaked %d talk_speakers", speakerRows)
	}

	var people int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM people WHERE id = $1", speaker).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 1 {
		t.Fatalf("fixture transaction unusable: %d people for id", people)
	}
}

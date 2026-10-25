package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertRegisteredSpeaker(t *testing.T, tx pgx.Tx, first, last, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name, email, google_sub) VALUES ($1, $2, $3, $4) RETURNING id",
		first, last, email, "sub-"+uuid.NewString()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func registeredTalkInput(speakerIDs ...uuid.UUID) domain.TalkInput {
	return domain.TalkInput{
		Title:       "Concurrency " + uuid.NewString(),
		Description: "Description",
		Location:    "Room 1",
		Language:    "en",
		Date:        time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC),
		Duration:    5400,
		SpeakerIDs:  speakerIDs,
	}
}

func countTalkRows(t *testing.T, tx pgx.Tx) int {
	t.Helper()
	var count int
	if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM talks").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestEndpointTalkCreateIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	ada := insertRegisteredSpeaker(t, tx, "Ada", "Lovelace", "ada@example.com")
	alan := insertRegisteredSpeaker(t, tx, "Alan", "Turing", "alan@example.com")

	input := registeredTalkInput(ada, alan)
	talk, err := repo.CreateTalk(ctx, input)
	if err != nil {
		t.Fatalf("create talk: %v", err)
	}
	if talk.ID == uuid.Nil || talk.Title != input.Title || talk.Description != input.Description ||
		talk.Location != input.Location || talk.Language != "en" || talk.Duration != 5400 || talk.Published {
		t.Fatalf("unexpected talk %+v", talk)
	}
	if !talk.Date.Equal(input.Date) || talk.Date.Location() != time.UTC {
		t.Fatalf("date %v, want %v", talk.Date, input.Date)
	}
	if talk.StreamURL != nil || talk.PosterURL != nil {
		t.Fatalf("optional urls %+v", talk)
	}
	if len(talk.Speakers) != 2 || talk.Speakers[0].ID != ada || talk.Speakers[1].ID != alan {
		t.Fatalf("speakers %+v", talk.Speakers)
	}
	if talk.CreatedAt.IsZero() || talk.UpdatedAt.IsZero() {
		t.Fatalf("audit timestamps %+v", talk)
	}

	var seconds int64
	var stream *string
	var speakers int
	err = tx.QueryRow(ctx,
		"SELECT extract(epoch from duration)::bigint, stream_url, (SELECT count(*) FROM talk_speakers WHERE talk_id = $1) "+
			"FROM talks WHERE id = $1", talk.ID).Scan(&seconds, &stream, &speakers)
	if err != nil {
		t.Fatal(err)
	}
	if seconds != 5400 || stream != nil || speakers != 2 {
		t.Fatalf("stored duration %d, stream %v, speakers %d", seconds, stream, speakers)
	}

	published := registeredTalkInput(ada)
	published.Title = "Published " + uuid.NewString()
	published.Published = true
	published.StreamURL = talkCreateTextPointer("https://example.com/stream")
	published.PosterURL = talkCreateTextPointer("https://example.com/poster.png")
	created, err := repo.CreateTalk(ctx, published)
	if err != nil {
		t.Fatalf("create published talk: %v", err)
	}
	if !created.Published || created.StreamURL == nil || *created.StreamURL != *published.StreamURL ||
		created.PosterURL == nil || *created.PosterURL != *published.PosterURL {
		t.Fatalf("published talk %+v", created)
	}
}

func TestEndpointTalkCreateDuplicateConflict(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertRegisteredSpeaker(t, tx, "Ada", "Lovelace", "ada@example.com")
	input := registeredTalkInput(speaker)
	if _, err := repo.CreateTalk(ctx, input); err != nil {
		t.Fatalf("create talk: %v", err)
	}
	duplicate := input
	duplicate.Description = "Other description"
	if _, err := repo.CreateTalk(ctx, duplicate); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate talk error %v, want conflict", err)
	}
	if count := countTalkRows(t, tx); count != 1 {
		t.Fatalf("conflicting create leaked rows: %d talks", count)
	}
}

func TestEndpointTalkCreateRejectsUnregisteredSpeakers(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	registered := insertRegisteredSpeaker(t, tx, "Ada", "Lovelace", "ada@example.com")
	unregistered := insertPersonRow(t, tx, "Grace", "Hopper")
	emailOnly := insertPersonRow(t, tx, "Katherine", "Johnson")
	if _, err := tx.Exec(ctx, "UPDATE people SET email = 'katherine@example.com' WHERE id = $1", emailOnly); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ids  []uuid.UUID
	}{
		{"empty", nil},
		{"nil id", []uuid.UUID{uuid.Nil}},
		{"nonexistent", []uuid.UUID{uuid.New()}},
		{"unregistered", []uuid.UUID{unregistered}},
		{"email without google sub", []uuid.UUID{emailOnly}},
		{"duplicate", []uuid.UUID{registered, registered}},
		{"mixed", []uuid.UUID{registered, uuid.New()}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := repo.CreateTalk(ctx, registeredTalkInput(testCase.ids...))
			var validation domain.ValidationError
			if !errors.As(err, &validation) || validation.Fields["speaker_ids"] == "" {
				t.Fatalf("error %v, want speaker_ids validation error", err)
			}
			if count := countTalkRows(t, tx); count != 0 {
				t.Fatalf("failed create leaked rows: %d talks", count)
			}
		})
	}
}

func talkCreateTextPointer(value string) *string {
	return &value
}

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func insertCalendarTalkRow(t *testing.T, tx pgx.Tx, id uuid.UUID, title, date string, published bool) {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO talks (id, title, description, date, duration, location, language, published) "+
			"VALUES ($1, $2, 'Description', $3::timestamp, '90 minutes', $4, 'es', $5)",
		pgUUID(id), title, date, "Calendar "+title, published); err != nil {
		t.Fatal(err)
	}
}

func linkCalendarSpeaker(t *testing.T, tx pgx.Tx, talkID, personID uuid.UUID) {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", pgUUID(talkID), pgUUID(personID)); err != nil {
		t.Fatal(err)
	}
}

func TestEndpointCalendarPostgresPublishedOrder(t *testing.T) {
	repository, tx := backendFixture(t)
	speaker := insertPersonRow(t, tx, "Ada", "Lovelace")
	winter := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tie := uuid.MustParse("00000000-0000-0000-0000-000000000005")
	summer := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	hidden := uuid.MustParse("00000000-0000-0000-0000-000000000000")
	insertCalendarTalkRow(t, tx, winter, "Winter", "2026-01-15 10:00:00", true)
	insertCalendarTalkRow(t, tx, tie, "Winter tie", "2026-01-15 10:00:00", true)
	insertCalendarTalkRow(t, tx, summer, "Summer", "2026-07-01 10:00:00", true)
	insertCalendarTalkRow(t, tx, hidden, "Hidden", "2025-01-01 10:00:00", false)
	for _, id := range []uuid.UUID{winter, tie, summer, hidden} {
		linkCalendarSpeaker(t, tx, id, speaker)
	}
	talks, err := repository.CalendarTalks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(talks) != 3 {
		t.Fatalf("talks %+v", talks)
	}
	order := []uuid.UUID{winter, tie, summer}
	for index, id := range order {
		if talks[index].ID != id {
			t.Fatalf("talk %d id %s, want %s", index, talks[index].ID, id)
		}
		if len(talks[index].Speakers) != 1 || talks[index].Speakers[0].ID != speaker {
			t.Fatalf("talk %d speakers %+v", index, talks[index].Speakers)
		}
		if talks[index].Duration != 5400 {
			t.Fatalf("talk %d duration %d", index, talks[index].Duration)
		}
	}
	if talks[0].Date.Location() != time.UTC ||
		!talks[0].Date.Equal(time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("winter date %v", talks[0].Date)
	}
	if !talks[2].Date.Equal(time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("summer date %v", talks[2].Date)
	}
	for _, talk := range talks {
		if talk.ID == hidden {
			t.Fatalf("unpublished talk leaked into calendar")
		}
	}
}

func TestEndpointCalendarPostgresEmpty(t *testing.T) {
	repository, _ := backendFixture(t)
	talks, err := repository.CalendarTalks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if talks == nil || len(talks) != 0 {
		t.Fatalf("talks %#v", talks)
	}
}

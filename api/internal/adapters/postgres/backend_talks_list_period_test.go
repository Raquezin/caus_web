package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertTalksListPeriodTalk(t *testing.T, tx pgx.Tx, title string, date time.Time, duration time.Duration) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO talks (title, description, date, duration, location, language, published) "+
			"VALUES ($1, 'Description', $2, make_interval(secs => $3), 'Room 1', 'es', true) RETURNING id",
		title, date, duration.Seconds()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEndpointTalksListPostgresPeriodUsesTalkEnd(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertPersonRow(t, tx, "Ada", "Lovelace")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	talks := []uuid.UUID{
		insertTalksListPeriodTalk(t, tx, "Ending Now", now.Add(-45*time.Minute), 45*time.Minute),
		insertTalksListPeriodTalk(t, tx, "In Progress", now.Add(-10*time.Minute), time.Hour),
		insertTalksListPeriodTalk(t, tx, "Future Soon", now.Add(time.Hour), 30*time.Minute),
		insertTalksListPeriodTalk(t, tx, "Future Late", now.Add(2*time.Hour), 30*time.Minute),
		insertTalksListPeriodTalk(t, tx, "Just Ended", now.Add(-30*time.Minute-time.Second), 30*time.Minute),
		insertTalksListPeriodTalk(t, tx, "Old Past", now.Add(-48*time.Hour), time.Hour),
	}
	for _, id := range talks {
		attachTalksListSpeaker(t, tx, id, speaker)
	}

	upcoming, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, upcoming.Talks, []string{"Ending Now", "In Progress", "Future Soon", "Future Late"})

	past, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodPast, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, past.Talks, []string{"Just Ended", "Old Past"})

	all, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, domain.TalkFilter{Period: domain.TalkPeriodAll, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, all.Talks,
		[]string{"Ending Now", "In Progress", "Future Soon", "Future Late", "Just Ended", "Old Past"})
}

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertTalksListTalk(t *testing.T, tx pgx.Tx, id uuid.UUID, title string, date time.Time, published bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	err := tx.QueryRow(ctx,
		"INSERT INTO talks (id, title, description, date, duration, location, language, published) "+
			"VALUES ($1, $2, 'Description', $3, interval '30 minutes', 'Room 1', 'es', $4) RETURNING id",
		id, title, date, published).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func attachTalksListSpeaker(t *testing.T, tx pgx.Tx, talkID, personID uuid.UUID) {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, personID); err != nil {
		t.Fatal(err)
	}
}

func talksListTitles(talks []domain.Talk) []string {
	titles := make([]string, 0, len(talks))
	for _, talk := range talks {
		titles = append(titles, talk.Title)
	}
	return titles
}

func assertTalksListTitles(t *testing.T, talks []domain.Talk, want []string) {
	t.Helper()
	got := talksListTitles(talks)
	if len(got) != len(want) {
		t.Fatalf("titles %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("titles %v, want %v", got, want)
		}
	}
}

func TestEndpointTalksListPostgresPeriods(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	first := insertPersonRow(t, tx, "Ada", "Lovelace")
	second := insertPersonRow(t, tx, "Grace", "Hopper")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	future := uuid.New()
	insertTalksListTalk(t, tx, future, "Future", time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, future, first)
	attachTalksListSpeaker(t, tx, future, second)
	tieHigh := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	insertTalksListTalk(t, tx, tieHigh, "Tie High", time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, tieHigh, first)
	tieLow := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	insertTalksListTalk(t, tx, tieLow, "Tie Low", time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, tieLow, first)
	near := uuid.New()
	insertTalksListTalk(t, tx, near, "Near", time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, near, first)
	boundary := uuid.New()
	insertTalksListTalk(t, tx, boundary, "Boundary", now, true)
	attachTalksListSpeaker(t, tx, boundary, first)
	recentPast := uuid.New()
	insertTalksListTalk(t, tx, recentPast, "Recent Past", time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, recentPast, first)
	oldPast := uuid.New()
	insertTalksListTalk(t, tx, oldPast, "Old Past", time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), true)
	attachTalksListSpeaker(t, tx, oldPast, first)
	hiddenFuture := uuid.New()
	insertTalksListTalk(t, tx, hiddenFuture, "Hidden Future", time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC), false)
	attachTalksListSpeaker(t, tx, hiddenFuture, first)
	hiddenPast := uuid.New()
	insertTalksListTalk(t, tx, hiddenPast, "Hidden Past", time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC), false)
	attachTalksListSpeaker(t, tx, hiddenPast, first)

	public := domain.TalkFilter{Period: domain.TalkPeriodAll, Now: now}
	page, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, public)
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore {
		t.Fatal("repository reported has_more")
	}
	assertTalksListTitles(t, page.Talks, []string{
		"Boundary", "Near", "Tie Low", "Tie High", "Future", "Recent Past", "Old Past",
	})

	upcoming, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, upcoming.Talks, []string{"Boundary", "Near", "Tie Low", "Tie High", "Future"})

	past, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, domain.TalkFilter{Period: domain.TalkPeriodPast, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, past.Talks, []string{"Recent Past", "Old Past"})

	admin, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, domain.TalkFilter{
		Period: domain.TalkPeriodAll, IncludeUnpublished: true, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, admin.Talks, []string{
		"Boundary", "Near", "Hidden Future", "Tie Low", "Tie High", "Future", "Hidden Past", "Recent Past", "Old Past",
	})
	if len(admin.Talks[5].Speakers) != 2 || admin.Talks[5].Speakers[0].LastName != "Hopper" ||
		admin.Talks[5].Speakers[1].LastName != "Lovelace" {
		t.Fatalf("hydrated speakers %+v", admin.Talks[5].Speakers)
	}
	if admin.Talks[0].Date.Location() != time.UTC || !admin.Talks[0].Date.Equal(now) {
		t.Fatalf("naive date %v", admin.Talks[0].Date)
	}
}

func TestEndpointTalksListPostgresPagination(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertPersonRow(t, tx, "Ada", "Lovelace")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for index, title := range []string{"First", "Second", "Third", "Fourth"} {
		id := uuid.New()
		insertTalksListTalk(t, tx, id, title, now.Add(time.Duration(index+1)*time.Hour), true)
		attachTalksListSpeaker(t, tx, id, speaker)
	}
	firstPage, err := repo.ListTalks(ctx, domain.Page{Limit: 2}, domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, firstPage.Talks, []string{"First", "Second"})
	if firstPage.HasMore {
		t.Fatal("repository reported has_more")
	}
	secondPage, err := repo.ListTalks(ctx, domain.Page{Limit: 2, Offset: 2},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, secondPage.Talks, []string{"Third", "Fourth"})
	empty, err := repo.ListTalks(ctx, domain.Page{Limit: 2, Offset: 4},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Talks == nil || len(empty.Talks) != 0 {
		t.Fatalf("empty page %+v", empty)
	}
	void, err := repo.ListTalks(ctx, domain.Page{Limit: 5},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now.Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if void.Talks == nil || len(void.Talks) != 0 {
		t.Fatalf("empty upcoming page %+v", void)
	}
}

func TestEndpointTalksListPostgresPastEqualDateTie(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tieDate := time.Date(2026, 2, 10, 9, 0, 0, 0, time.UTC)
	tieHigh := uuid.MustParse("00000000-0000-0000-0000-00000000001b")
	insertTalksListTalk(t, tx, tieHigh, "Tie High", tieDate, true)
	tieLow := uuid.MustParse("00000000-0000-0000-0000-00000000001a")
	insertTalksListTalk(t, tx, tieLow, "Tie Low", tieDate, true)
	insertTalksListTalk(t, tx, uuid.New(), "Future", now.Add(24*time.Hour), true)
	insertTalksListTalk(t, tx, uuid.New(), "Older", tieDate.Add(-24*time.Hour), true)

	filter := domain.TalkFilter{Period: domain.TalkPeriodPast, Now: now}
	page, err := repo.ListTalks(ctx, domain.Page{Limit: 20}, filter)
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, page.Talks, []string{"Tie Low", "Tie High", "Older"})

	for offset, want := range map[int]string{0: "Tie Low", 1: "Tie High", 2: "Older"} {
		window, err := repo.ListTalks(ctx, domain.Page{Limit: 1, Offset: offset}, filter)
		if err != nil {
			t.Fatal(err)
		}
		assertTalksListTitles(t, window.Talks, []string{want})
	}
}

func TestEndpointTalksListPostgresAdminVisibility(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	insertTalksListTalk(t, tx, uuid.New(), "Public Future", time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), true)
	insertTalksListTalk(t, tx, uuid.New(), "Draft Future", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), false)
	insertTalksListTalk(t, tx, uuid.New(), "Public Past", time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), true)
	insertTalksListTalk(t, tx, uuid.New(), "Draft Past", time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC), false)

	publicUpcoming, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, publicUpcoming.Talks, []string{"Public Future"})

	adminUpcoming, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodUpcoming, IncludeUnpublished: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, adminUpcoming.Talks, []string{"Draft Future", "Public Future"})
	if adminUpcoming.Talks[0].Published {
		t.Fatal("admin upcoming list lost the unpublished draft")
	}

	publicPast, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodPast, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, publicPast.Talks, []string{"Public Past"})

	adminPast, err := repo.ListTalks(ctx, domain.Page{Limit: 20},
		domain.TalkFilter{Period: domain.TalkPeriodPast, IncludeUnpublished: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	assertTalksListTitles(t, adminPast.Talks, []string{"Draft Past", "Public Past"})
}

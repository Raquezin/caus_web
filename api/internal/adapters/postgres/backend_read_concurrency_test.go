package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

type concurrentWriteHookDB struct {
	BackendDB
	match string
	hook  func()
	fired bool
}

func (d *concurrentWriteHookDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !d.fired && strings.Contains(sql, d.match) {
		d.fired = true
		d.hook()
	}
	return d.BackendDB.Query(ctx, sql, args...)
}

type calendarConcurrencyFixture struct {
	speaker   uuid.UUID
	kept      uuid.UUID
	withdrawn uuid.UUID
}

func newCalendarConcurrencyFixture(t *testing.T, tx pgx.Tx) calendarConcurrencyFixture {
	t.Helper()
	speaker := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.test", "sub-ada", "member")
	return calendarConcurrencyFixture{
		speaker:   speaker,
		kept:      insertExportTalk(t, tx, "Kept", "2027-01-10 18:00", true, speaker),
		withdrawn: insertExportTalk(t, tx, "Withdrawn", "2027-01-11 18:00", true, speaker),
	}
}

func assertCalendarKeptTalk(t *testing.T, talks []domain.Talk, fixture calendarConcurrencyFixture) {
	t.Helper()
	if len(talks) != 2 || talks[0].ID != fixture.kept || talks[1].ID != fixture.withdrawn {
		t.Fatalf("calendar talks %+v", talks)
	}
	if len(talks[0].Speakers) != 1 || talks[0].Speakers[0].ID != fixture.speaker ||
		talks[0].Speakers[0].LastName != "Lovelace" {
		t.Fatalf("kept talk speakers %+v", talks[0].Speakers)
	}
}

func TestCalendarTalksSurvivesConcurrentUnpublish(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	fixture := newCalendarConcurrencyFixture(t, tx)
	db := &concurrentWriteHookDB{BackendDB: tx, match: "FROM talk_speakers", hook: func() {
		if _, err := tx.Exec(ctx, "UPDATE talks SET published = false WHERE id = $1", fixture.withdrawn); err != nil {
			t.Fatal(err)
		}
	}}

	talks, err := NewBackendRepository(db).CalendarTalks(ctx)
	if err != nil {
		t.Fatalf("calendar feed failed after a concurrent unpublish: %v", err)
	}
	if !db.fired {
		t.Fatal("concurrent unpublish never ran")
	}
	assertCalendarKeptTalk(t, talks, fixture)
}

func TestCalendarTalksSurvivesConcurrentDelete(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	fixture := newCalendarConcurrencyFixture(t, tx)
	db := &concurrentWriteHookDB{BackendDB: tx, match: "FROM talk_speakers", hook: func() {
		if _, err := tx.Exec(ctx, "DELETE FROM talks WHERE id = $1", fixture.withdrawn); err != nil {
			t.Fatal(err)
		}
	}}

	talks, err := NewBackendRepository(db).CalendarTalks(ctx)
	if err != nil {
		t.Fatalf("calendar feed failed after a concurrent delete: %v", err)
	}
	if !db.fired {
		t.Fatal("concurrent delete never ran")
	}
	assertCalendarKeptTalk(t, talks, fixture)
}

type teamConcurrencyFixture struct {
	competition uuid.UUID
	kept        uuid.UUID
	removed     uuid.UUID
	member      uuid.UUID
}

func newTeamConcurrencyFixture(t *testing.T, tx pgx.Tx) teamConcurrencyFixture {
	t.Helper()
	member := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.test", "sub-ada", "member")
	other := insertExportPerson(t, tx, "Alan", "Turing", "alan@example.test", "sub-alan", "member")
	competition := insertExportCompetition(t, tx, "Cup")
	kept := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	removed := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	insertConcurrencyTeam(t, tx, competition, kept, "Kept", member)
	insertConcurrencyTeam(t, tx, competition, removed, "Removed", other)
	return teamConcurrencyFixture{competition: competition, kept: kept, removed: removed, member: member}
}

func insertConcurrencyTeam(t *testing.T, tx pgx.Tx, competition, team uuid.UUID, name string, member uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if _, err := tx.Exec(ctx, "INSERT INTO teams (id, competition_id, name) VALUES ($1, $2, $3)",
		team, competition, name); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		team, competition, member); err != nil {
		t.Fatal(err)
	}
}

func assertTeamConcurrencyKept(t *testing.T, teams []domain.Team, fixture teamConcurrencyFixture) {
	t.Helper()
	if len(teams) != 2 {
		t.Fatalf("teams %+v", teams)
	}
	for _, team := range teams {
		if team.ID != fixture.kept {
			continue
		}
		if len(team.Members) != 1 || team.Members[0].ID != fixture.member ||
			team.Members[0].FirstName != "Ada" || team.Members[0].LastName != "Lovelace" {
			t.Fatalf("kept team members %+v", team.Members)
		}
		return
	}
	t.Fatalf("kept team missing from %+v", teams)
}

func newTeamDeleteHook(t *testing.T, tx pgx.Tx, removed uuid.UUID) *concurrentWriteHookDB {
	t.Helper()
	ctx := context.Background()
	return &concurrentWriteHookDB{BackendDB: tx, match: "FROM team_members", hook: func() {
		if _, err := tx.Exec(ctx, "DELETE FROM teams WHERE id = $1", removed); err != nil {
			t.Fatal(err)
		}
	}}
}

func TestGetCompetitionSurvivesConcurrentTeamDelete(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	fixture := newTeamConcurrencyFixture(t, tx)
	db := newTeamDeleteHook(t, tx, fixture.removed)

	detail, err := NewBackendRepository(db).GetCompetition(ctx, fixture.competition)
	if err != nil {
		t.Fatalf("competition detail failed after a concurrent team delete: %v", err)
	}
	if !db.fired {
		t.Fatal("concurrent team delete never ran")
	}
	if detail.Competition.ID != fixture.competition {
		t.Fatalf("competition %+v", detail.Competition)
	}
	assertTeamConcurrencyKept(t, detail.Teams, fixture)
}

func TestListTeamsSurvivesConcurrentTeamDelete(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	fixture := newTeamConcurrencyFixture(t, tx)
	db := newTeamDeleteHook(t, tx, fixture.removed)

	page, err := NewBackendRepository(db).ListTeams(ctx, fixture.competition, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("team list failed after a concurrent team delete: %v", err)
	}
	if !db.fired {
		t.Fatal("concurrent team delete never ran")
	}
	assertTeamConcurrencyKept(t, page.Teams, fixture)
}

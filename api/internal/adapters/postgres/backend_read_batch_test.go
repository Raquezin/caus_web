package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"caus-platform/api/internal/domain"
)

type countingBackendDB struct {
	BackendDB
	queries    int
	queryRows  int
	executions int
}

func (d *countingBackendDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.queries++
	return d.BackendDB.Query(ctx, sql, args...)
}

func (d *countingBackendDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.queryRows++
	return d.BackendDB.QueryRow(ctx, sql, args...)
}

func (d *countingBackendDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.executions++
	return d.BackendDB.Exec(ctx, sql, args...)
}

func TestListTalksBatchesSpeakerReads(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.test", "sub-ada", "member")
	for _, title := range []string{"One", "Two", "Three"} {
		insertExportTalk(t, tx, title, "2027-01-10 18:00", true, speaker)
	}
	db := &countingBackendDB{BackendDB: tx}
	page, err := NewBackendRepository(db).ListTalks(ctx,
		domain.Page{Limit: 10}, domain.TalkFilter{Period: domain.TalkPeriodAll})
	if err != nil {
		t.Fatal(err)
	}
	if db.queries != 2 {
		t.Fatalf("ListTalks issued %d Query calls, want 2", db.queries)
	}
	if len(page.Talks) != 3 {
		t.Fatalf("talks %+v", page.Talks)
	}
	for _, talk := range page.Talks {
		if len(talk.Speakers) != 1 || talk.Speakers[0].ID != speaker ||
			talk.Speakers[0].FirstName != "Ada" || talk.Speakers[0].LastName != "Lovelace" {
			t.Fatalf("talk %s speakers %+v", talk.Title, talk.Speakers)
		}
	}
}

func TestListTalksEmptyIssuesSingleQuery(t *testing.T) {
	_, tx := backendFixture(t)
	db := &countingBackendDB{BackendDB: tx}
	page, err := NewBackendRepository(db).ListTalks(context.Background(),
		domain.Page{Limit: 10}, domain.TalkFilter{Period: domain.TalkPeriodAll})
	if err != nil {
		t.Fatal(err)
	}
	if db.queries != 1 {
		t.Fatalf("empty ListTalks issued %d Query calls, want 1", db.queries)
	}
	if len(page.Talks) != 0 {
		t.Fatalf("talks %+v", page.Talks)
	}
}

func TestCalendarTalksBatchesSpeakerReads(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	speaker := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.test", "sub-ada", "member")
	insertExportTalk(t, tx, "First", "2027-01-10 18:00", true, speaker)
	insertExportTalk(t, tx, "Second", "2027-01-11 18:00", true, speaker)
	db := &countingBackendDB{BackendDB: tx}
	talks, err := NewBackendRepository(db).CalendarTalks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if db.queries != 2 {
		t.Fatalf("CalendarTalks issued %d Query calls, want 2", db.queries)
	}
	for _, talk := range talks {
		if len(talk.Speakers) != 1 || talk.Speakers[0].ID != speaker {
			t.Fatalf("talk %s speakers %+v", talk.Title, talk.Speakers)
		}
	}
}

func TestGetCompetitionBatchesMemberReads(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	members := []uuid.UUID{
		insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.test", "sub-ada", "member"),
		insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.test", "sub-grace", "member"),
		insertExportPerson(t, tx, "Alan", "Turing", "alan@example.test", "sub-alan", "member"),
	}
	competition := insertExportCompetition(t, tx, "Cup")
	for index, name := range []string{"Alpha", "Beta", "Gamma"} {
		insertExportTeam(t, tx, competition, name, members[index])
	}
	db := &countingBackendDB{BackendDB: tx}
	detail, err := NewBackendRepository(db).GetCompetition(ctx, competition)
	if err != nil {
		t.Fatal(err)
	}
	if db.queryRows != 1 || db.queries != 2 {
		t.Fatalf("GetCompetition issued %d QueryRow and %d Query calls, want 1 and 2", db.queryRows, db.queries)
	}
	if len(detail.Teams) != 3 {
		t.Fatalf("teams %+v", detail.Teams)
	}
	want := map[uuid.UUID]bool{members[0]: true, members[1]: true, members[2]: true}
	for _, team := range detail.Teams {
		if len(team.Members) != 1 || !want[team.Members[0].ID] {
			t.Fatalf("team %v members %+v", team.Name, team.Members)
		}
		delete(want, team.Members[0].ID)
	}
}

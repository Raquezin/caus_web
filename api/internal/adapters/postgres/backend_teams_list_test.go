package postgres

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

type teamsListMember struct {
	id    uuid.UUID
	first string
	last  string
}

type teamsListTeam struct {
	id      uuid.UUID
	members []teamsListMember
}

func teamsListCompetition(t *testing.T, tx pgx.Tx, teamSize int) uuid.UUID {
	t.Helper()
	var competitionID uuid.UUID
	if err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', $2) RETURNING id",
		"Competition "+uuid.NewString(), teamSize).Scan(&competitionID); err != nil {
		t.Fatal(err)
	}
	return competitionID
}

func teamsListInsert(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, teamID uuid.UUID, position *int, members [][2]string) teamsListTeam {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO teams (id, competition_id, name, position) VALUES ($1, $2, $3, $4)",
		teamID, competitionID, "Team "+uuid.NewString(), position); err != nil {
		t.Fatal(err)
	}
	team := teamsListTeam{id: teamID}
	for _, member := range members {
		memberID := insertPersonRow(t, tx, member[0], member[1])
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, memberID); err != nil {
			t.Fatal(err)
		}
		team.members = append(team.members, teamsListMember{id: memberID, first: member[0], last: member[1]})
	}
	sort.Slice(team.members, func(i, j int) bool {
		if team.members[i].last != team.members[j].last {
			return team.members[i].last < team.members[j].last
		}
		if team.members[i].first != team.members[j].first {
			return team.members[i].first < team.members[j].first
		}
		return team.members[i].id.String() < team.members[j].id.String()
	})
	return team
}

func teamsListIDs(teams []domain.Team) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(teams))
	for _, team := range teams {
		ids = append(ids, team.ID)
	}
	return ids
}

func teamsListFixture(t *testing.T, tx pgx.Tx) (uuid.UUID, []teamsListTeam) {
	t.Helper()
	competitionID := teamsListCompetition(t, tx, 3)
	positionOne, positionTwo := 1, 2
	unpositioned := teamsListInsert(t, tx, competitionID, uuid.MustParse("99999999-9999-4999-8999-999999999999"),
		nil, [][2]string{{"Ada", "Lovelace"}})
	third := teamsListInsert(t, tx, competitionID, uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		&positionTwo, [][2]string{{"Alan", "Turing"}})
	second := teamsListInsert(t, tx, competitionID, uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		&positionOne, [][2]string{{"Grace", "Hopper"}})
	first := teamsListInsert(t, tx, competitionID, uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		&positionOne, [][2]string{{"Zoe", "Zulu"}, {"Able", "Able"}})
	return competitionID, []teamsListTeam{first, second, third, unpositioned}
}

func teamsListAssertMembers(t *testing.T, team domain.Team, want teamsListTeam) {
	t.Helper()
	if team.CompetitionID == uuid.Nil {
		t.Fatalf("team %v has no competition id", team.ID)
	}
	if len(team.Members) != len(want.members) {
		t.Fatalf("team %v has %d members, want %d", team.ID, len(team.Members), len(want.members))
	}
	for i, member := range want.members {
		got := team.Members[i]
		if got.ID != member.id || got.FirstName != member.first || got.LastName != member.last {
			t.Fatalf("team %v member %d = %+v, want %+v", team.ID, i, got, member)
		}
	}
}

func TestEndpointTeamsListIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID, want := teamsListFixture(t, tx)

	page, err := repo.ListTeams(ctx, competitionID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore {
		t.Fatal("unexpected has_more")
	}
	if len(page.Teams) != len(want) {
		t.Fatalf("got %d teams, want %d", len(page.Teams), len(want))
	}
	for i, team := range page.Teams {
		if team.ID != want[i].id {
			t.Fatalf("team %d is %v, want %v (order %v)", i, team.ID, want[i].id, teamsListIDs(page.Teams))
		}
		if team.CompetitionID != competitionID {
			t.Fatalf("team %v competition %v", team.ID, team.CompetitionID)
		}
		if i < len(want)-1 && (team.Position == nil || *team.Position < 1) {
			t.Fatalf("team %d position %v", i, team.Position)
		}
		if i == len(want)-1 && team.Position != nil {
			t.Fatalf("unpositioned team sorted with position %v", *team.Position)
		}
		teamsListAssertMembers(t, team, want[i])
	}
}

func TestEndpointTeamsListIDTiebreakIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := teamsListCompetition(t, tx, 3)
	positionOne := 1
	lowerID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	higherID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	teamsListInsert(t, tx, competitionID, higherID, &positionOne, [][2]string{{"First", "Person"}})
	teamsListInsert(t, tx, competitionID, lowerID, &positionOne, [][2]string{{"Second", "Person"}})

	page, err := repo.ListTeams(ctx, competitionID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Teams) != 2 || page.Teams[0].ID != lowerID || page.Teams[1].ID != higherID {
		t.Fatalf("tiebreak order %v, want %v then %v", teamsListIDs(page.Teams), lowerID, higherID)
	}
}

func TestEndpointTeamsListBoundedSliceIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID, want := teamsListFixture(t, tx)

	page, err := repo.ListTeams(ctx, competitionID, domain.Page{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Teams) != 2 {
		t.Fatalf("window size %d, want 2", len(page.Teams))
	}
	for i, team := range page.Teams {
		if team.ID != want[i+1].id {
			t.Fatalf("window %v, want %v and %v", teamsListIDs(page.Teams), want[1].id, want[2].id)
		}
	}
}

func TestEndpointTeamsListMissingAndEmptyIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	if _, err := repo.ListTeams(ctx, uuid.New(), domain.Page{Limit: 10}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing parent error %v", err)
	}

	emptyID := teamsListCompetition(t, tx, 3)
	page, err := repo.ListTeams(ctx, emptyID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Teams == nil || len(page.Teams) != 0 || page.HasMore {
		t.Fatalf("empty result %+v", page)
	}

	fixtureID, _ := teamsListFixture(t, tx)
	page, err = repo.ListTeams(ctx, fixtureID, domain.Page{Limit: 10, Offset: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Teams == nil || len(page.Teams) != 0 || page.HasMore {
		t.Fatalf("past-end result %+v", page)
	}
}

func TestEndpointTeamsListClosesRowsBeforeReadTeamIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID, _ := teamsListFixture(t, tx)

	if _, err := repo.ListTeams(ctx, competitionID, domain.Page{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("transaction unusable after ListTeams: %v", err)
	}
}

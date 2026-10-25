package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func teamCreatePtr[T any](value T) *T {
	return &value
}

func insertTeamCreateCompetition(t *testing.T, tx pgx.Tx, teamSize int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', $2) RETURNING id",
		"Competition "+uuid.NewString(), teamSize).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertTeamCreateTeam(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, name string, memberIDs ...uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(context.Background(),
		"INSERT INTO teams (competition_id, name) VALUES ($1, $2) RETURNING id",
		competitionID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, memberID := range memberIDs {
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			id, competitionID, memberID); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func teamCreateRowCount(t *testing.T, tx pgx.Tx, table string) int {
	t.Helper()
	var query string
	switch table {
	case "teams":
		query = "SELECT count(*) FROM teams"
	case "team_members":
		query = "SELECT count(*) FROM team_members"
	default:
		t.Fatalf("unsupported table %q", table)
	}
	var count int
	if err := tx.QueryRow(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func teamCreateAssertTxWorks(t *testing.T, tx pgx.Tx) {
	t.Helper()
	var one int
	if err := tx.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("fixture tx unusable: %v", err)
	}
	if one != 1 {
		t.Fatalf("fixture tx returned %d", one)
	}
}

func teamCreateMemberIDs(team domain.Team) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(team.Members))
	for _, member := range team.Members {
		ids = append(ids, member.ID)
	}
	return ids
}

func TestEndpointTeamCreateRepositoryIndividualUnnamed(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	member := insertPersonRow(t, tx, "Ada", "Lovelace")
	insertPersonRow(t, tx, "Grace", "Hopper")

	team, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{MemberIDs: []uuid.UUID{member}})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.ID == uuid.Nil || team.CompetitionID != competitionID {
		t.Fatalf("team identity %+v", team)
	}
	if team.Name != nil || team.PhotoURL != nil || team.Position != nil {
		t.Fatalf("nullable fields %v %v %v", team.Name, team.PhotoURL, team.Position)
	}
	if len(team.Members) != 1 || team.Members[0].ID != member ||
		team.Members[0].FirstName != "Ada" || team.Members[0].LastName != "Lovelace" {
		t.Fatalf("members %+v", team.Members)
	}
	if team.CreatedAt.IsZero() || team.UpdatedAt.IsZero() {
		t.Fatalf("timestamps %+v", team)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 1 {
		t.Fatalf("teams count %d", count)
	}
	var joins int
	if err := tx.QueryRow(ctx,
		"SELECT count(*) FROM team_members WHERE competition_id = $1", competitionID).Scan(&joins); err != nil {
		t.Fatal(err)
	}
	if joins != 1 {
		t.Fatalf("team_members count %d", joins)
	}
}

func TestEndpointTeamCreateRepositoryFullTeam(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	ada := insertPersonRow(t, tx, "Ada", "Lovelace")
	grace := insertPersonRow(t, tx, "Grace", "Hopper")

	team, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{
		Name:      teamCreatePtr("Team A"),
		PhotoURL:  teamCreatePtr("https://example.com/p.png"),
		Position:  teamCreatePtr(2),
		MemberIDs: []uuid.UUID{ada, grace},
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.Name == nil || *team.Name != "Team A" {
		t.Fatalf("name %v", team.Name)
	}
	if team.PhotoURL == nil || *team.PhotoURL != "https://example.com/p.png" {
		t.Fatalf("photo %v", team.PhotoURL)
	}
	if team.Position == nil || *team.Position != 2 {
		t.Fatalf("position %v", team.Position)
	}
	members := teamCreateMemberIDs(team)
	if len(members) != 2 || members[0] != grace || members[1] != ada {
		t.Fatalf("members %v want [%s %s]", members, grace, ada)
	}
	if count := teamCreateRowCount(t, tx, "team_members"); count != 2 {
		t.Fatalf("team_members count %d", count)
	}
}

func TestEndpointTeamCreateRepositoryRejectsEmptyMembership(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	for _, memberIDs := range [][]uuid.UUID{nil, {}} {
		_, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{MemberIDs: memberIDs})
		var validation domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("empty membership error %v", err)
		}
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 0 {
		t.Fatalf("teams count %d after empty membership", count)
	}
	if count := teamCreateRowCount(t, tx, "team_members"); count != 0 {
		t.Fatalf("team_members count %d after empty membership", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

func TestEndpointTeamCreateRepositoryRejectsOverfullMembership(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	first := insertPersonRow(t, tx, "Ada", "Lovelace")
	second := insertPersonRow(t, tx, "Grace", "Hopper")
	third := insertPersonRow(t, tx, "Alan", "Turing")

	_, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{MemberIDs: []uuid.UUID{first, second, third}})
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("overfull membership error %v", err)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 0 {
		t.Fatalf("teams count %d after overfull membership", count)
	}
	if count := teamCreateRowCount(t, tx, "team_members"); count != 0 {
		t.Fatalf("team_members count %d after overfull membership", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

func TestEndpointTeamCreateRepositoryRejectsUnknownPerson(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)

	_, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{MemberIDs: []uuid.UUID{uuid.New()}})
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("unknown person error %v", err)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 0 {
		t.Fatalf("teams count %d after unknown person", count)
	}
	if count := teamCreateRowCount(t, tx, "team_members"); count != 0 {
		t.Fatalf("team_members count %d after unknown person", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

func TestEndpointTeamCreateRepositoryRejectsDuplicateParticipation(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	ada := insertPersonRow(t, tx, "Ada", "Lovelace")
	grace := insertPersonRow(t, tx, "Grace", "Hopper")
	insertTeamCreateTeam(t, tx, competitionID, "Team A", ada)

	_, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{
		Name: teamCreatePtr("Team B"), MemberIDs: []uuid.UUID{ada, grace}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate participation error %v", err)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 1 {
		t.Fatalf("teams count %d after duplicate participation", count)
	}
	if count := teamCreateRowCount(t, tx, "team_members"); count != 1 {
		t.Fatalf("team_members count %d after duplicate participation", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

func TestEndpointTeamCreateRepositoryRejectsDuplicateName(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertTeamCreateCompetition(t, tx, 2)
	ada := insertPersonRow(t, tx, "Ada", "Lovelace")
	grace := insertPersonRow(t, tx, "Grace", "Hopper")
	insertTeamCreateTeam(t, tx, competitionID, "Team A", ada)

	_, err := repo.CreateTeam(ctx, competitionID, domain.TeamInput{
		Name: teamCreatePtr("Team A"), MemberIDs: []uuid.UUID{grace}})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate name error %v", err)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 1 {
		t.Fatalf("teams count %d after duplicate name", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

func TestEndpointTeamCreateRepositoryMissingCompetition(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	member := insertPersonRow(t, tx, "Ada", "Lovelace")

	_, err := repo.CreateTeam(ctx, uuid.New(), domain.TeamInput{MemberIDs: []uuid.UUID{member}})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing competition error %v", err)
	}
	if count := teamCreateRowCount(t, tx, "teams"); count != 0 {
		t.Fatalf("teams count %d after missing competition", count)
	}
	teamCreateAssertTxWorks(t, tx)
}

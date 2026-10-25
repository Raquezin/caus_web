package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func teamDeleteAddMembers(t *testing.T, tx pgx.Tx, competitionID, teamID uuid.UUID, members ...uuid.UUID) {
	t.Helper()
	for _, member := range members {
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, member); err != nil {
			t.Fatalf("add member %v: %v", member, err)
		}
	}
}

func teamDeleteCount(t *testing.T, tx pgx.Tx, query string, args ...any) int {
	t.Helper()
	var count int
	if err := tx.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

func TestEndpointTeamDeleteRepositoryIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	first := insertCompetition(t, tx, "First", 2)
	second := insertCompetition(t, tx, "Second", 2)
	shared := insertPerson(t, tx)
	kept := insertPerson(t, tx)

	doomed := insertTeam(t, tx, first, "Doomed")
	teamDeleteAddMembers(t, tx, first, doomed, shared)
	survivor := insertTeam(t, tx, first, "Survivor")
	teamDeleteAddMembers(t, tx, first, survivor, kept)
	elsewhere := insertTeam(t, tx, second, "Elsewhere")
	teamDeleteAddMembers(t, tx, second, elsewhere, shared, kept)

	if err := repo.DeleteTeam(ctx, doomed); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if count := teamDeleteCount(t, tx, "SELECT count(*) FROM teams WHERE id = $1", doomed); count != 0 {
		t.Fatalf("deleted team still present: %d", count)
	}
	if count := teamDeleteCount(t, tx, "SELECT count(*) FROM team_members WHERE team_id = $1", doomed); count != 0 {
		t.Fatalf("deleted team memberships survived: %d", count)
	}
	for _, team := range []struct {
		name    string
		id      uuid.UUID
		members int
	}{
		{"sibling team", survivor, 1},
		{"other competition team", elsewhere, 2},
	} {
		if count := teamDeleteCount(t, tx, "SELECT count(*) FROM teams WHERE id = $1", team.id); count != 1 {
			t.Fatalf("%s deleted: %d", team.name, count)
		}
		if count := teamDeleteCount(t, tx, "SELECT count(*) FROM team_members WHERE team_id = $1", team.id); count != team.members {
			t.Fatalf("%s memberships %d, want %d", team.name, count, team.members)
		}
	}
	for _, competition := range []uuid.UUID{first, second} {
		if count := teamDeleteCount(t, tx, "SELECT count(*) FROM competitions WHERE id = $1", competition); count != 1 {
			t.Fatalf("competition %v deleted: %d", competition, count)
		}
	}
	for _, person := range []uuid.UUID{shared, kept} {
		if count := teamDeleteCount(t, tx, "SELECT count(*) FROM people WHERE id = $1", person); count != 1 {
			t.Fatalf("person %v deleted: %d", person, count)
		}
	}

	if err := repo.DeleteTeam(ctx, doomed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("repeat delete error %v, want not found", err)
	}
	for _, missing := range []uuid.UUID{uuid.New(), uuid.Nil} {
		if err := repo.DeleteTeam(ctx, missing); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing team %v error %v, want not found", missing, err)
		}
	}

	replacement := insertTeam(t, tx, first, "Replacement")
	teamDeleteAddMembers(t, tx, first, replacement, shared)
	flushConstraints(t, tx)
	if count := teamDeleteCount(t, tx,
		"SELECT count(*) FROM team_members WHERE competition_id = $1 AND person_id = $2", first, shared); count != 1 {
		t.Fatalf("shared member participations %d, want 1", count)
	}
	if count := teamDeleteCount(t, tx,
		"SELECT count(*) FROM team_members WHERE team_id = $1", replacement); count != 1 {
		t.Fatalf("replacement memberships %d, want 1", count)
	}
}

func TestEndpointTeamDeleteRepositoryRespectsCancelledContext(t *testing.T) {
	repo, tx := backendFixture(t)
	first := insertCompetition(t, tx, "First", 2)
	member := insertPerson(t, tx)
	team := insertTeam(t, tx, first, "Cancelled")
	teamDeleteAddMembers(t, tx, first, team, member)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repo.DeleteTeam(ctx, team); err == nil {
		t.Fatal("cancelled context deleted the team")
	}
	if count := teamDeleteCount(t, tx, "SELECT count(*) FROM teams WHERE id = $1", team); count != 1 {
		t.Fatalf("cancelled delete removed the team: %d", count)
	}
	if count := teamDeleteCount(t, tx, "SELECT count(*) FROM team_members WHERE team_id = $1", team); count != 1 {
		t.Fatalf("cancelled delete removed memberships: %d", count)
	}
}

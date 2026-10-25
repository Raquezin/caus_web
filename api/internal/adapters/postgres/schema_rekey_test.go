package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestTalksRequireSpeakerOnIDRekeyIntegration(t *testing.T) {
	t.Run("empty talk rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		insertTalk(t, tx)
		expectCheckViolation(t, tx)
	})
	t.Run("single rekey rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		talkID := insertTalk(t, tx)
		if _, err := tx.Exec(context.Background(), "UPDATE talks SET id = $1 WHERE id = $2", uuid.New(), talkID); err != nil {
			t.Fatal(err)
		}
		expectCheckViolation(t, tx)
	})
	t.Run("multiple rekeys rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		talkID := insertTalk(t, tx)
		for i := 0; i < 3; i++ {
			next := uuid.New()
			if _, err := tx.Exec(context.Background(), "UPDATE talks SET id = $1 WHERE id = $2", next, talkID); err != nil {
				t.Fatal(err)
			}
			talkID = next
		}
		expectCheckViolation(t, tx)
	})
	t.Run("speaker added after rekey passes", func(t *testing.T) {
		tx := schemaFixture(t)
		talkID := insertTalk(t, tx)
		newID := uuid.New()
		if _, err := tx.Exec(context.Background(), "UPDATE talks SET id = $1 WHERE id = $2", newID, talkID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", newID, insertPerson(t, tx)); err != nil {
			t.Fatal(err)
		}
		flushConstraints(t, tx)
	})
	t.Run("inserted then deleted talk passes", func(t *testing.T) {
		tx := schemaFixture(t)
		talkID := insertTalk(t, tx)
		if _, err := tx.Exec(context.Background(), "DELETE FROM talks WHERE id = $1", talkID); err != nil {
			t.Fatal(err)
		}
		flushConstraints(t, tx)
	})
}

func TestTeamsRequireMembersOnRekeyIntegration(t *testing.T) {
	t.Run("empty team rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		insertTeam(t, tx, insertCompetition(t, tx, "comp-a", 3), "team-a")
		expectCheckViolation(t, tx)
	})
	t.Run("single rekey rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		teamID := insertTeam(t, tx, insertCompetition(t, tx, "comp-a", 3), "team-a")
		if _, err := tx.Exec(context.Background(), "UPDATE teams SET id = $1 WHERE id = $2", uuid.New(), teamID); err != nil {
			t.Fatal(err)
		}
		expectCheckViolation(t, tx)
	})
	t.Run("multiple rekeys rejected", func(t *testing.T) {
		tx := schemaFixture(t)
		teamID := insertTeam(t, tx, insertCompetition(t, tx, "comp-a", 3), "team-a")
		for i := 0; i < 3; i++ {
			next := uuid.New()
			if _, err := tx.Exec(context.Background(), "UPDATE teams SET id = $1 WHERE id = $2", next, teamID); err != nil {
				t.Fatal(err)
			}
			teamID = next
		}
		expectCheckViolation(t, tx)
	})
	t.Run("member added after rekey passes", func(t *testing.T) {
		tx := schemaFixture(t)
		competitionID := insertCompetition(t, tx, "comp-a", 3)
		teamID := insertTeam(t, tx, competitionID, "team-a")
		newID := uuid.New()
		if _, err := tx.Exec(context.Background(), "UPDATE teams SET id = $1 WHERE id = $2", newID, teamID); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			newID, competitionID, insertPerson(t, tx)); err != nil {
			t.Fatal(err)
		}
		flushConstraints(t, tx)
	})
	t.Run("inserted then deleted team passes", func(t *testing.T) {
		tx := schemaFixture(t)
		teamID := insertTeam(t, tx, insertCompetition(t, tx, "comp-a", 3), "team-a")
		if _, err := tx.Exec(context.Background(), "DELETE FROM teams WHERE id = $1", teamID); err != nil {
			t.Fatal(err)
		}
		flushConstraints(t, tx)
	})
}

func TestTeamMoveAcrossCompetitionsIntegration(t *testing.T) {
	fixture := func(t *testing.T) (pgx.Tx, uuid.UUID, uuid.UUID, uuid.UUID) {
		tx := schemaFixture(t)
		small := insertCompetition(t, tx, "comp-small", 1)
		large := insertCompetition(t, tx, "comp-large", 3)
		target := insertCompetition(t, tx, "comp-target", 3)
		teamID := insertTeam(t, tx, large, "team-a")
		for i := 0; i < 3; i++ {
			if _, err := tx.Exec(context.Background(),
				"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
				teamID, large, insertPerson(t, tx)); err != nil {
				t.Fatal(err)
			}
		}
		flushConstraints(t, tx)
		return tx, teamID, small, target
	}
	move := "WITH moved_team AS (UPDATE teams SET competition_id = $1 WHERE id = $2 RETURNING id) " +
		"UPDATE team_members SET competition_id = $1 WHERE team_id IN (SELECT id FROM moved_team)"
	t.Run("move to undersized competition rejected", func(t *testing.T) {
		tx, teamID, small, _ := fixture(t)
		if _, err := tx.Exec(context.Background(), move, small, teamID); err != nil {
			t.Fatal(err)
		}
		expectCheckViolation(t, tx)
	})
	t.Run("move to sufficiently sized competition passes", func(t *testing.T) {
		tx, teamID, _, target := fixture(t)
		if _, err := tx.Exec(context.Background(), move, target, teamID); err != nil {
			t.Fatal(err)
		}
		flushConstraints(t, tx)
	})
}

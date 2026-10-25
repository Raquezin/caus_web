package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointTeamGetIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	var competitionID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ('Spring Cup', 'Description', '2026-05-02 18:00:00', 'Hall', 3) RETURNING id").Scan(&competitionID); err != nil {
		t.Fatal(err)
	}

	hopper := insertPersonRow(t, tx, "Grace", "Hopper")
	lovelace := insertPersonRow(t, tx, "Ada", "Lovelace")
	nullableMember := insertPersonRow(t, tx, "Katherine", "Johnson")
	for personID, email := range map[uuid.UUID]string{
		hopper:         "grace.hopper@example.test",
		lovelace:       "ada.lovelace@example.test",
		nullableMember: "katherine.johnson@example.test",
	} {
		if _, err := tx.Exec(ctx,
			"UPDATE people SET email = $1, google_sub = $2 WHERE id = $3",
			email, "sub-"+personID.String(), personID); err != nil {
			t.Fatal(err)
		}
	}

	var teamID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO teams (competition_id, name, position, photo_url) "+
			"VALUES ($1, 'Team Alpha', 3, 'https://example.test/photo.png') RETURNING id",
		competitionID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	for _, memberID := range []uuid.UUID{hopper, lovelace} {
		if _, err := tx.Exec(ctx,
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, memberID); err != nil {
			t.Fatal(err)
		}
	}

	var nullableTeamID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO teams (competition_id) VALUES ($1) RETURNING id",
		competitionID).Scan(&nullableTeamID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		nullableTeamID, competitionID, nullableMember); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	team, err := repo.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != teamID {
		t.Fatalf("team id %s, want %s", team.ID, teamID)
	}
	if team.CompetitionID != competitionID {
		t.Fatalf("competition id %s, want %s", team.CompetitionID, competitionID)
	}
	if team.Name == nil || *team.Name != "Team Alpha" {
		t.Fatalf("name %v", team.Name)
	}
	if team.PhotoURL == nil || *team.PhotoURL != "https://example.test/photo.png" {
		t.Fatalf("photo url %v", team.PhotoURL)
	}
	if team.Position == nil || *team.Position != 3 {
		t.Fatalf("position %v", team.Position)
	}
	if team.CreatedAt.IsZero() || team.UpdatedAt.IsZero() {
		t.Fatalf("timestamps %v %v", team.CreatedAt, team.UpdatedAt)
	}
	if len(team.Members) != 2 {
		t.Fatalf("members %+v", team.Members)
	}
	if team.Members[0].ID != hopper || team.Members[0].FirstName != "Grace" || team.Members[0].LastName != "Hopper" {
		t.Fatalf("first member %+v", team.Members[0])
	}
	if team.Members[1].ID != lovelace || team.Members[1].FirstName != "Ada" || team.Members[1].LastName != "Lovelace" {
		t.Fatalf("second member %+v", team.Members[1])
	}
	for _, member := range team.Members {
		if member.FirstName == "" || member.LastName == "" {
			t.Fatalf("member missing names: %+v", member)
		}
	}

	nullable, err := repo.GetTeam(ctx, nullableTeamID)
	if err != nil {
		t.Fatal(err)
	}
	if nullable.ID != nullableTeamID || nullable.CompetitionID != competitionID {
		t.Fatalf("nullable team ids %+v", nullable)
	}
	if nullable.Name != nil || nullable.PhotoURL != nil || nullable.Position != nil {
		t.Fatalf("nullable columns populated: %+v", nullable)
	}
	if nullable.Members == nil || len(nullable.Members) != 1 {
		t.Fatalf("nullable team members %+v (nil %v)", nullable.Members, nullable.Members == nil)
	}
	if nullable.Members[0].ID != nullableMember ||
		nullable.Members[0].FirstName != "Katherine" ||
		nullable.Members[0].LastName != "Johnson" {
		t.Fatalf("nullable team member %+v", nullable.Members[0])
	}

	if _, err := repo.GetTeam(ctx, uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing team: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.GetTeam(cancelled, teamID); err == nil {
		t.Fatal("cancelled context returned no error")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error %v", err)
	}
}

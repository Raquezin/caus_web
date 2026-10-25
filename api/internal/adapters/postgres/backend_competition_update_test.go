package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func insertCompetitionUpdateCompetition(t *testing.T, tx pgx.Tx, name string, teamSize int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', $2) RETURNING id", name, teamSize).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCompetitionUpdateTeam(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, members int) uuid.UUID {
	t.Helper()
	var teamID uuid.UUID
	if err := tx.QueryRow(context.Background(),
		"INSERT INTO teams (competition_id) VALUES ($1) RETURNING id", competitionID).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	for range members {
		personID := insertPersonRow(t, tx, "First", "Last "+uuid.NewString())
		if _, err := tx.Exec(context.Background(),
			"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
			teamID, competitionID, personID); err != nil {
			t.Fatal(err)
		}
	}
	return teamID
}

func competitionUpdateValidationFields(t *testing.T, err error) map[string]string {
	t.Helper()
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %v is not a validation error", err)
	}
	return validation.Fields
}

func TestEndpointCompetitionUpdateRepositoryPreservesOmittedFields(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertCompetitionUpdateCompetition(t, tx, "Spring", 3)
	insertCompetitionUpdateTeam(t, tx, competitionID, 3)

	name := "Autumn"
	updated, err := repo.UpdateCompetition(ctx, competitionID, domain.CompetitionPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Autumn" || updated.Description != "Description" || updated.Location != "Hall" || updated.TeamSize != 3 {
		t.Fatalf("partial update result %+v", updated)
	}
	if !updated.Date.Equal(time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC)) || updated.Date.Location() != time.UTC {
		t.Fatalf("preserved date %v", updated.Date)
	}
	date := time.Date(2027, 1, 9, 21, 15, 0, 0, time.UTC)
	updated, err = repo.UpdateCompetition(ctx, competitionID, domain.CompetitionPatch{Date: &date})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Date.Equal(date) || updated.Name != "Autumn" {
		t.Fatalf("date update result %+v", updated)
	}
	persisted, err := readCompetition(ctx, tx, competitionID)
	if err != nil || !persisted.Date.Equal(date) || persisted.Date.Location() != time.UTC {
		t.Fatalf("persisted date %v (%v)", persisted.Date, err)
	}
}

func TestEndpointCompetitionUpdateRepositoryValidatesPatch(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertCompetitionUpdateCompetition(t, tx, "Spring", 3)

	if fields := competitionUpdateValidationFields(t, mustCompetitionUpdateError(t, repo, competitionID, domain.CompetitionPatch{})); fields["patch"] == "" {
		t.Fatalf("empty patch fields %v", fields)
	}
	zero := 0
	if fields := competitionUpdateValidationFields(t, mustCompetitionUpdateError(t, repo, competitionID, domain.CompetitionPatch{TeamSize: &zero})); fields["team_size"] == "" {
		t.Fatalf("zero team size fields %v", fields)
	}
	var zeroDate time.Time
	if fields := competitionUpdateValidationFields(t, mustCompetitionUpdateError(t, repo, competitionID, domain.CompetitionPatch{Date: &zeroDate})); fields["date"] == "" {
		t.Fatalf("zero date fields %v", fields)
	}
	persisted, err := readCompetition(ctx, tx, competitionID)
	if err != nil || persisted.TeamSize != 3 || persisted.Name != "Spring" {
		t.Fatalf("invalid patch changed competition %+v (%v)", persisted, err)
	}
	missingName := "Autumn"
	if _, err := repo.UpdateCompetition(ctx, uuid.New(), domain.CompetitionPatch{Name: &missingName}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing competition error %v", err)
	}
}

func mustCompetitionUpdateError(t *testing.T, repo *BackendRepository, id uuid.UUID, patch domain.CompetitionPatch) error {
	t.Helper()
	_, err := repo.UpdateCompetition(context.Background(), id, patch)
	if err == nil {
		t.Fatal("patch accepted")
	}
	return err
}

func TestEndpointCompetitionUpdateRepositoryShrinkRollsBackAllFields(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertCompetitionUpdateCompetition(t, tx, "Spring", 3)
	insertCompetitionUpdateTeam(t, tx, competitionID, 3)

	grown := 5
	date := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	description := "Updated"
	updated, err := repo.UpdateCompetition(ctx, competitionID,
		domain.CompetitionPatch{TeamSize: &grown, Date: &date, Description: &description})
	if err != nil || updated.TeamSize != 5 || !updated.Date.Equal(date) || updated.Description != "Updated" {
		t.Fatalf("growth result %+v (%v)", updated, err)
	}
	shrunk := 3
	if updated, err = repo.UpdateCompetition(ctx, competitionID, domain.CompetitionPatch{TeamSize: &shrunk}); err != nil || updated.TeamSize != 3 {
		t.Fatalf("valid shrink %+v (%v)", updated, err)
	}

	tooSmall := 2
	name := "Winter"
	err = mustCompetitionUpdateError(t, repo, competitionID, domain.CompetitionPatch{TeamSize: &tooSmall, Name: &name})
	if fields := competitionUpdateValidationFields(t, err); len(fields) == 0 {
		t.Fatal("shrink failure carried no field")
	}
	persisted, err := readCompetition(ctx, tx, competitionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.TeamSize != 3 || persisted.Name != "Spring" || persisted.Description != "Updated" || !persisted.Date.Equal(date) {
		t.Fatalf("failed shrink was not fully rolled back: %+v", persisted)
	}
}

func TestEndpointCompetitionUpdateRepositoryRejectsDuplicate(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := insertCompetitionUpdateCompetition(t, tx, "Spring", 3)
	insertCompetitionUpdateCompetition(t, tx, "Winter", 3)

	duplicateDate := time.Date(2026, 5, 2, 18, 0, 0, 0, time.UTC)
	duplicateName := "Winter"
	err := mustCompetitionUpdateError(t, repo, competitionID,
		domain.CompetitionPatch{Name: &duplicateName, Date: &duplicateDate})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate error %v", err)
	}
	persisted, err := readCompetition(ctx, tx, competitionID)
	if err != nil || persisted.Name != "Spring" {
		t.Fatalf("conflict changed competition %+v (%v)", persisted, err)
	}
}

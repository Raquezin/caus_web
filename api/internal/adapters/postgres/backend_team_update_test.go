package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func endpointTeamUpdateCompetition(t *testing.T, tx pgx.Tx, size int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) "+
			"VALUES ($1, 'Description', '2026-05-02 18:00:00', 'Hall', $2) RETURNING id",
		"Competition "+uuid.NewString(), size).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func endpointTeamUpdateTeamRow(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, name string,
	photoURL *string, position *int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO teams (competition_id, name, photo_url, position) VALUES ($1, $2, $3, $4) RETURNING id",
		competitionID, name, photoURL, position).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func endpointTeamUpdateMember(t *testing.T, tx pgx.Tx, teamID, competitionID, personID uuid.UUID) {
	t.Helper()
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		teamID, competitionID, personID); err != nil {
		t.Fatal(err)
	}
}

func endpointTeamUpdateMemberIDs(team domain.Team) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(team.Members))
	for _, member := range team.Members {
		ids = append(ids, member.ID)
	}
	return ids
}

func TestEndpointTeamUpdateScalarsMembersAndNullableIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := endpointTeamUpdateCompetition(t, tx, 3)
	photo := "https://example.test/photo.png"
	position := 2
	teamID := endpointTeamUpdateTeamRow(t, tx, competitionID, "Team A", &photo, &position)
	memberA := insertPersonRow(t, tx, "Ada", "Lovelace")
	memberB := insertPersonRow(t, tx, "Grace", "Hopper")
	endpointTeamUpdateMember(t, tx, teamID, competitionID, memberA)

	name := "Team Updated"
	updatedPhoto := "https://example.test/updated.png"
	updatedPosition := 5
	updated, err := repo.UpdateTeam(ctx, teamID, domain.TeamPatch{
		Name:      &domain.NullableText{Value: &name},
		PhotoURL:  &domain.NullableText{Value: &updatedPhoto},
		Position:  &domain.NullableInt{Value: &updatedPosition},
		MemberIDs: &[]uuid.UUID{memberB, memberA},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != teamID || updated.CompetitionID != competitionID || updated.Name == nil || *updated.Name != name ||
		updated.PhotoURL == nil || *updated.PhotoURL != updatedPhoto || updated.Position == nil || *updated.Position != updatedPosition {
		t.Fatalf("updated scalars %+v", updated)
	}
	if len(updated.Members) != 2 || !reflect.DeepEqual(endpointTeamUpdateMemberIDs(updated), []uuid.UUID{memberB, memberA}) {
		t.Fatalf("updated members %+v", updated.Members)
	}

	onlyName := "Only Name"
	updated, err = repo.UpdateTeam(ctx, teamID, domain.TeamPatch{Name: &domain.NullableText{Value: &onlyName}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name == nil || *updated.Name != onlyName || updated.PhotoURL == nil || *updated.PhotoURL != updatedPhoto ||
		updated.Position == nil || *updated.Position != updatedPosition || len(updated.Members) != 2 {
		t.Fatalf("omitted fields changed %+v", updated)
	}

	updated, err = repo.UpdateTeam(ctx, teamID, domain.TeamPatch{
		Name:     &domain.NullableText{},
		PhotoURL: &domain.NullableText{},
		Position: &domain.NullableInt{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != nil || updated.PhotoURL != nil || updated.Position != nil {
		t.Fatalf("nullable fields not cleared %+v", updated)
	}
	if len(updated.Members) != 2 || updated.CompetitionID != competitionID {
		t.Fatalf("nullable patch changed members or competition %+v", updated)
	}
	persisted, err := readTeam(ctx, tx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != nil || persisted.PhotoURL != nil || persisted.Position != nil || len(persisted.Members) != 2 {
		t.Fatalf("persisted team %+v", persisted)
	}
}

func TestEndpointTeamUpdateRollsBackInvalidChangesIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	competitionID := endpointTeamUpdateCompetition(t, tx, 2)
	teamID := endpointTeamUpdateTeamRow(t, tx, competitionID, "Team A", nil, nil)
	otherTeamID := endpointTeamUpdateTeamRow(t, tx, competitionID, "Team B", nil, nil)
	memberA := insertPersonRow(t, tx, "Ada", "Lovelace")
	memberB := insertPersonRow(t, tx, "Grace", "Hopper")
	memberC := insertPersonRow(t, tx, "Alan", "Turing")
	memberD := insertPersonRow(t, tx, "Katherine", "Johnson")
	endpointTeamUpdateMember(t, tx, teamID, competitionID, memberA)
	endpointTeamUpdateMember(t, tx, otherTeamID, competitionID, memberB)
	original, err := readTeam(ctx, tx, teamID)
	if err != nil {
		t.Fatal(err)
	}
	name := "Changed"
	zero := 0
	failing := []struct {
		name  string
		patch domain.TeamPatch
		check func(error) bool
	}{
		{"empty patch", domain.TeamPatch{}, func(err error) bool {
			var validation domain.ValidationError
			return errors.As(err, &validation)
		}},
		{"zero position", domain.TeamPatch{Name: &domain.NullableText{Value: &name}, Position: &domain.NullableInt{Value: &zero}},
			func(err error) bool {
				var validation domain.ValidationError
				return errors.As(err, &validation) && validation.Fields["position"] != ""
			}},
		{"empty members", domain.TeamPatch{Name: &domain.NullableText{Value: &name}, MemberIDs: &[]uuid.UUID{}},
			func(err error) bool {
				var validation domain.ValidationError
				return errors.As(err, &validation) && validation.Fields["member_ids"] != ""
			}},
		{"duplicate members", domain.TeamPatch{Name: &domain.NullableText{Value: &name},
			MemberIDs: &[]uuid.UUID{memberA, memberA}}, func(err error) bool {
			var validation domain.ValidationError
			return errors.As(err, &validation) && validation.Fields["member_ids"] != ""
		}},
		{"overfull", domain.TeamPatch{Name: &domain.NullableText{Value: &name},
			MemberIDs: &[]uuid.UUID{memberA, memberC, memberD}}, func(err error) bool {
			var validation domain.ValidationError
			return errors.As(err, &validation) && validation.Fields["member_ids"] != ""
		}},
		{"missing person", domain.TeamPatch{Name: &domain.NullableText{Value: &name},
			MemberIDs: &[]uuid.UUID{memberA, uuid.New()}}, func(err error) bool {
			var validation domain.ValidationError
			return errors.As(err, &validation)
		}},
		{"cross-team duplicate", domain.TeamPatch{Name: &domain.NullableText{Value: &name},
			MemberIDs: &[]uuid.UUID{memberA, memberB}}, func(err error) bool {
			return errors.Is(err, domain.ErrConflict)
		}},
	}
	for _, testCase := range failing {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := repo.UpdateTeam(ctx, teamID, testCase.patch); !testCase.check(err) {
				t.Fatalf("error %v did not match", err)
			}
			unchanged, err := readTeam(ctx, tx, teamID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(unchanged, original) {
				t.Fatalf("failed update changed team: %+v, want %+v", unchanged, original)
			}
		})
	}
	if _, err := repo.UpdateTeam(ctx, uuid.New(), domain.TeamPatch{Name: &domain.NullableText{Value: &name}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing team error %v", err)
	}
}

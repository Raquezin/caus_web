package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) CreateTeam(ctx context.Context, competitionID uuid.UUID, input domain.TeamInput) (domain.Team, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	var lockedID uuid.UUID
	if err := tx.QueryRow(ctx,
		"SELECT id FROM competitions WHERE id = $1 FOR UPDATE", pgUUID(competitionID)).Scan(&lockedID); err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	var teamID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO teams (competition_id, name, position, photo_url) VALUES ($1, $2, $3, $4) RETURNING id",
		pgUUID(competitionID), input.Name, input.Position, input.PhotoURL).Scan(&teamID); err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	memberIDs := make([]pgtype.UUID, 0, len(input.MemberIDs))
	for _, memberID := range input.MemberIDs {
		memberIDs = append(memberIDs, pgUUID(memberID))
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO team_members (team_id, competition_id, person_id) SELECT $1, $2, unnest($3::uuid[])",
		pgUUID(teamID), pgUUID(competitionID), memberIDs); err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	team, err := readTeam(ctx, tx, teamID)
	if err != nil {
		return domain.Team{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Team{}, err
	}
	return team, nil
}

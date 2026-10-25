package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

const competitionUpdateStatement = "UPDATE competitions SET name = $2, description = $3, location = $4, " +
	"date = $5, team_size = $6 WHERE id = $1"

func (r *BackendRepository) UpdateCompetition(ctx context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked pgtype.UUID
	err = tx.QueryRow(ctx, "SELECT id FROM competitions WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Competition{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	current, err := readCompetition(ctx, tx, id)
	if err != nil {
		return domain.Competition{}, err
	}
	merged, err := application.ApplyCompetitionPatch(domain.CompetitionInput{
		Name:        current.Name,
		Description: current.Description,
		Location:    current.Location,
		Date:        current.Date,
		TeamSize:    current.TeamSize,
	}, patch)
	if err != nil {
		return domain.Competition{}, err
	}
	if _, err := tx.Exec(ctx, competitionUpdateStatement, pgUUID(id), merged.Name, merged.Description,
		merged.Location, naiveTimestamp(merged.Date), merged.TeamSize); err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	updated, err := readCompetition(ctx, tx, id)
	if err != nil {
		return domain.Competition{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Competition{}, err
	}
	return updated, nil
}

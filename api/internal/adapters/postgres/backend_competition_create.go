package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) CreateCompetition(ctx context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)

	var id pgtype.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO competitions (name, description, date, location, team_size) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		input.Name, input.Description, naiveTimestamp(input.Date), input.Location, input.TeamSize).Scan(&id); err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	competition, err := readCompetition(ctx, tx, id.Bytes)
	if err != nil {
		return domain.Competition{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Competition{}, err
	}
	return competition, nil
}

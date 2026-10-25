package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	var competitionID pgtype.UUID
	if err := tx.QueryRow(ctx,
		"SELECT competition_id FROM teams WHERE id = $1", pgUUID(id)).Scan(&competitionID); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx,
		"SELECT id FROM competitions WHERE id = $1 FOR UPDATE", competitionID); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx,
		"SELECT id FROM teams WHERE id = $1 FOR UPDATE", pgUUID(id)); err != nil {
		return translateBackendError(err)
	}
	tag, err := tx.Exec(ctx, "DELETE FROM teams WHERE id = $1", pgUUID(id))
	if err != nil {
		return translateBackendError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return finishBackendTx(ctx, tx)
}

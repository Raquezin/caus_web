package postgres

import (
	"context"

	"github.com/google/uuid"
)

func (r *BackendRepository) DeleteCompetition(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM competitions WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&locked); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM competitions WHERE id = $1", pgUUID(id)); err != nil {
		return translateBackendError(err)
	}
	return finishBackendTx(ctx, tx)
}

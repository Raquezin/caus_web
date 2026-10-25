package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) DeleteTalk(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "DELETE FROM talks WHERE id = $1", pgUUID(id))
	if err != nil {
		return translateBackendError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return err
	}
	return nil
}

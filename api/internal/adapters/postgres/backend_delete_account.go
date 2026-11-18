package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *BackendRepository) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	if err := requireAnotherAdmin(ctx, tx, id); err != nil {
		return err
	}

	var personID pgtype.UUID
	if err := tx.QueryRow(ctx,
		"SELECT id FROM people WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&personID); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE person_id = $1", pgUUID(id)); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx,
		"UPDATE people SET email = NULL, google_sub = NULL, role = 'member' WHERE id = $1", pgUUID(id)); err != nil {
		return translateBackendError(err)
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return err
	}
	return nil
}

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func finishBackendTx(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		return translateBackendError(err)
	}
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return translateBackendError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return translateBackendError(err)
	}
	return nil
}

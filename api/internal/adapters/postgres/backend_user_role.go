package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) UpdateUserRole(ctx context.Context, id uuid.UUID, role string) (domain.User, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.User{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	var updated pgtype.UUID
	err = tx.QueryRow(ctx, "UPDATE people SET role = $2::text::person_role WHERE id = $1 RETURNING id",
		pgUUID(id), role).Scan(&updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, translateBackendError(err)
	}
	user, err := readUser(ctx, tx, updated.Bytes)
	if err != nil {
		return domain.User{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) UpdateAccount(ctx context.Context, id uuid.UUID, patch domain.AccountPatch) (domain.Account, error) {
	account, err := scanAccount(r.db.QueryRow(ctx,
		"UPDATE people SET first_name = coalesce($2, first_name), last_name = coalesce($3, last_name) "+
			"WHERE id = $1 AND google_sub IS NOT NULL RETURNING "+accountColumns,
		pgUUID(id), patch.FirstName, patch.LastName))
	if err != nil {
		return domain.Account{}, translateBackendError(err)
	}
	return account, nil
}

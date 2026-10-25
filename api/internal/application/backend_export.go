package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) ExportAccount(ctx context.Context, id uuid.UUID) (domain.AccountExport, error) {
	if id == uuid.Nil {
		return domain.AccountExport{}, domain.ErrNotFound
	}
	export, err := b.repo.ExportAccount(ctx, id)
	if err != nil {
		return domain.AccountExport{}, err
	}
	if export.Account.ID == uuid.Nil {
		return domain.AccountExport{}, domain.ErrNotFound
	}
	return export, nil
}

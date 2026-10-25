package application

import (
	"context"

	"github.com/google/uuid"
)

func (b *Backend) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	return b.repo.DeleteAccount(ctx, id)
}

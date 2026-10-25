package application

import (
	"context"

	"github.com/google/uuid"
)

func (b *Backend) DeleteTalk(ctx context.Context, id uuid.UUID) error {
	return b.repo.DeleteTalk(ctx, id)
}

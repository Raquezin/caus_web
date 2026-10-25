package application

import (
	"context"

	"github.com/google/uuid"
)

func (b *Backend) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	return b.repo.DeleteTeam(ctx, id)
}

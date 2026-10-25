package application

import (
	"context"

	"github.com/google/uuid"
)

func (b *Backend) DeleteCompetition(ctx context.Context, id uuid.UUID) error {
	return b.repo.DeleteCompetition(ctx, id)
}

package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) GetTeam(ctx context.Context, id uuid.UUID) (domain.Team, error) {
	return b.repo.GetTeam(ctx, id)
}

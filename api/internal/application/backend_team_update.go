package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) UpdateTeam(ctx context.Context, id uuid.UUID, patch domain.TeamPatch) (domain.Team, error) {
	return b.repo.UpdateTeam(ctx, id, patch)
}

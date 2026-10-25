package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error) {
	return b.repo.GetCompetition(ctx, id)
}

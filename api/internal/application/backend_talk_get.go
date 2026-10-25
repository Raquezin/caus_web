package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	return b.repo.GetTalk(ctx, id, includeUnpublished)
}

package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) UpdateTalk(ctx context.Context, id uuid.UUID, patch domain.TalkPatch) (domain.Talk, error) {
	return b.repo.UpdateTalk(ctx, id, patch)
}

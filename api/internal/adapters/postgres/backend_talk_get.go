package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	return readTalk(ctx, r.db, id, includeUnpublished)
}

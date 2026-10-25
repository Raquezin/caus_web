package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) GetTeam(ctx context.Context, id uuid.UUID) (domain.Team, error) {
	return readTeam(ctx, r.db, id)
}

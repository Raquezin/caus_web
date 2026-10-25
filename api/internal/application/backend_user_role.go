package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) UpdateUserRole(ctx context.Context, id uuid.UUID, role string) (domain.User, error) {
	if err := ValidateRole(role); err != nil {
		return domain.User{}, err
	}
	return b.repo.UpdateUserRole(ctx, id, role)
}

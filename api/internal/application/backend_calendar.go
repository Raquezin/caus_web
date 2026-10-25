package application

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (b *Backend) CalendarTalks(ctx context.Context) ([]domain.Talk, error) {
	return b.repo.CalendarTalks(ctx)
}

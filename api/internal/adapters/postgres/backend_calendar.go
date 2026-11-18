package postgres

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) CalendarTalks(ctx context.Context) ([]domain.Talk, error) {
	return queryTalks(ctx, r.db, "SELECT "+talkColumns+" FROM talks WHERE published ORDER BY date, id")
}

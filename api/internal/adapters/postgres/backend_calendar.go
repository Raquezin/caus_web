package postgres

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) CalendarTalks(ctx context.Context) ([]domain.Talk, error) {
	ids, err := queryIDs(ctx, r.db, "SELECT id FROM talks WHERE published ORDER BY date, id")
	if err != nil {
		return nil, err
	}
	talks := make([]domain.Talk, 0, len(ids))
	for _, id := range ids {
		talk, err := readTalk(ctx, r.db, id, false)
		if err != nil {
			return nil, err
		}
		talks = append(talks, talk)
	}
	return talks, nil
}

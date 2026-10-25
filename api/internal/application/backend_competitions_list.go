package application

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (b *Backend) ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error) {
	if err := ValidatePage(page); err != nil {
		return domain.CompetitionPage{}, err
	}
	fetched, err := b.repo.ListCompetitions(ctx, domain.Page{Limit: page.Limit + 1, Offset: page.Offset})
	if err != nil {
		return domain.CompetitionPage{}, err
	}
	competitions := fetched.Competitions
	if competitions == nil {
		competitions = []domain.Competition{}
	}
	hasMore := len(competitions) > page.Limit
	if hasMore {
		competitions = competitions[:page.Limit]
	}
	return domain.CompetitionPage{Competitions: competitions, HasMore: hasMore}, nil
}

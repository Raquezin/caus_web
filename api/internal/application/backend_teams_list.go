package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) ListTeams(ctx context.Context, competitionID uuid.UUID, page domain.Page) (domain.TeamPage, error) {
	if err := ValidatePage(page); err != nil {
		return domain.TeamPage{}, err
	}
	fetched, err := b.repo.ListTeams(ctx, competitionID, domain.Page{Limit: page.Limit + 1, Offset: page.Offset})
	if err != nil {
		return domain.TeamPage{}, err
	}
	teams := fetched.Teams
	if teams == nil {
		teams = []domain.Team{}
	}
	hasMore := len(teams) > page.Limit
	if hasMore {
		teams = teams[:page.Limit]
	}
	return domain.TeamPage{Teams: teams, HasMore: hasMore}, nil
}

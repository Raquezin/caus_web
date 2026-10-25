package application

import (
	"context"

	"caus-platform/api/internal/domain"
)

func (b *Backend) CreateCompetition(ctx context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	if err := ValidateCompetitionInput(input); err != nil {
		return domain.Competition{}, err
	}
	return b.repo.CreateCompetition(ctx, input)
}

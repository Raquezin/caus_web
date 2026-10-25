package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) CreateTeam(ctx context.Context, competitionID uuid.UUID, input domain.TeamInput) (domain.Team, error) {
	if err := ValidateTeamInput(input); err != nil {
		return domain.Team{}, err
	}
	return b.repo.CreateTeam(ctx, competitionID, input)
}

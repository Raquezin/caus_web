package application

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (b *Backend) UpdateCompetition(ctx context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error) {
	if err := validateCompetitionPatchFields(patch); err != nil {
		return domain.Competition{}, err
	}
	return b.repo.UpdateCompetition(ctx, id, patch)
}

func validateCompetitionPatchFields(patch domain.CompetitionPatch) error {
	if patch.Name == nil && patch.Description == nil && patch.Location == nil && patch.Date == nil && patch.TeamSize == nil {
		return emptyPatchError()
	}
	fields := map[string]string{}
	if patch.Name != nil {
		requireName(fields, "name", *patch.Name)
	}
	if patch.Description != nil {
		requireText(fields, "description", *patch.Description)
	}
	if patch.Location != nil {
		requireName(fields, "location", *patch.Location)
	}
	if patch.Date != nil && patch.Date.IsZero() {
		fields["date"] = "date is required"
	}
	if patch.TeamSize != nil && (*patch.TeamSize < 1 || *patch.TeamSize > maxTeamSize) {
		fields["team_size"] = "team_size must be between 1 and 32767"
	}
	return domain.NewValidationError(fields)
}

package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error) {
	competition, err := readCompetition(ctx, r.db, id)
	if err != nil {
		return domain.CompetitionDetail{}, err
	}
	teams, err := queryTeams(ctx, r.db,
		"SELECT "+teamColumns+" FROM teams WHERE competition_id = $1 ORDER BY position ASC NULLS LAST, id", pgUUID(id))
	if err != nil {
		return domain.CompetitionDetail{}, err
	}
	return domain.CompetitionDetail{Competition: competition, Teams: teams}, nil
}

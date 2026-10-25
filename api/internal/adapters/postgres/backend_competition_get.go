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
	teamIDs, err := queryIDs(ctx, r.db,
		"SELECT id FROM teams WHERE competition_id = $1 ORDER BY position ASC NULLS LAST, id", pgUUID(id))
	if err != nil {
		return domain.CompetitionDetail{}, err
	}
	teams := make([]domain.Team, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		team, err := readTeam(ctx, r.db, teamID)
		if err != nil {
			return domain.CompetitionDetail{}, err
		}
		teams = append(teams, team)
	}
	return domain.CompetitionDetail{Competition: competition, Teams: teams}, nil
}

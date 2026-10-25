package postgres

import (
	"context"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

const teamListQuery = "SELECT id FROM teams WHERE competition_id = $1 ORDER BY position ASC NULLS LAST, id ASC LIMIT $2 OFFSET $3"

func (r *BackendRepository) ListTeams(ctx context.Context, competitionID uuid.UUID, page domain.Page) (domain.TeamPage, error) {
	if _, err := readCompetition(ctx, r.db, competitionID); err != nil {
		return domain.TeamPage{}, err
	}
	ids, err := queryIDs(ctx, r.db, teamListQuery, pgUUID(competitionID), page.Limit, page.Offset)
	if err != nil {
		return domain.TeamPage{}, err
	}
	teams := make([]domain.Team, 0, len(ids))
	for _, id := range ids {
		team, err := readTeam(ctx, r.db, id)
		if err != nil {
			return domain.TeamPage{}, err
		}
		teams = append(teams, team)
	}
	return domain.TeamPage{Teams: teams, HasMore: false}, nil
}

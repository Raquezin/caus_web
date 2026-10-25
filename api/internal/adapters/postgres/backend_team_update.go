package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) UpdateTeam(ctx context.Context, id uuid.UUID, patch domain.TeamPatch) (domain.Team, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	competitionID, err := teamUpdateReadCompetitionID(ctx, tx, id)
	if err != nil {
		return domain.Team{}, err
	}
	teamSize, err := teamUpdateLockCompetition(ctx, tx, competitionID)
	if err != nil {
		return domain.Team{}, err
	}
	lockedCompetitionID, err := teamUpdateLockTeam(ctx, tx, id)
	if err != nil {
		return domain.Team{}, err
	}
	if lockedCompetitionID != competitionID {
		return domain.Team{}, domain.ErrNotFound
	}
	team, err := readTeam(ctx, tx, id)
	if err != nil {
		return domain.Team{}, err
	}
	if team.CompetitionID != competitionID {
		return domain.Team{}, domain.ErrNotFound
	}
	current := domain.TeamInput{
		Name:      team.Name,
		PhotoURL:  team.PhotoURL,
		Position:  team.Position,
		MemberIDs: teamUpdateMemberIDs(team.Members),
	}
	merged, err := application.ApplyTeamPatch(current, patch)
	if err != nil {
		return domain.Team{}, err
	}
	if len(merged.MemberIDs) > teamSize {
		return domain.Team{}, domain.ValidationError{Fields: map[string]string{
			"member_ids": "member_ids must contain at most team_size people",
		}}
	}
	if _, err := tx.Exec(ctx, "UPDATE teams SET name = $2, photo_url = $3, position = $4 WHERE id = $1",
		pgUUID(id), merged.Name, merged.PhotoURL, merged.Position); err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	if patch.MemberIDs != nil {
		if _, err := tx.Exec(ctx, "DELETE FROM team_members WHERE team_id = $1", pgUUID(id)); err != nil {
			return domain.Team{}, translateBackendError(err)
		}
		for _, personID := range merged.MemberIDs {
			if _, err := tx.Exec(ctx,
				"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
				pgUUID(id), pgUUID(competitionID), pgUUID(personID)); err != nil {
				return domain.Team{}, translateBackendError(err)
			}
		}
	}
	updated, err := readTeam(ctx, tx, id)
	if err != nil {
		return domain.Team{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Team{}, err
	}
	return updated, nil
}

func teamUpdateReadCompetitionID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	var competitionID uuid.UUID
	err := tx.QueryRow(ctx, "SELECT competition_id FROM teams WHERE id = $1", pgUUID(id)).Scan(&competitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, translateBackendError(err)
	}
	return competitionID, nil
}

func teamUpdateLockCompetition(ctx context.Context, tx pgx.Tx, id uuid.UUID) (int, error) {
	var teamSize int
	err := tx.QueryRow(ctx, "SELECT team_size FROM competitions WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&teamSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, translateBackendError(err)
	}
	return teamSize, nil
}

func teamUpdateLockTeam(ctx context.Context, tx pgx.Tx, id uuid.UUID) (uuid.UUID, error) {
	var competitionID uuid.UUID
	err := tx.QueryRow(ctx, "SELECT competition_id FROM teams WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&competitionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, translateBackendError(err)
	}
	return competitionID, nil
}

func teamUpdateMemberIDs(members []domain.Person) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.ID)
	}
	return ids
}

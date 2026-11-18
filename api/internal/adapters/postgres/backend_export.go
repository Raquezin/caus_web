package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) ExportAccount(ctx context.Context, id uuid.UUID) (domain.AccountExport, error) {
	if id == uuid.Nil {
		return domain.AccountExport{}, domain.ErrNotFound
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.AccountExport{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return domain.AccountExport{}, translateBackendError(err)
	}
	export, err := readAccountExport(ctx, tx, id)
	if err != nil {
		return domain.AccountExport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AccountExport{}, translateBackendError(err)
	}
	return export, nil
}

func readAccountExport(ctx context.Context, q Querier, id uuid.UUID) (domain.AccountExport, error) {
	export := domain.AccountExport{
		Sessions:     []domain.SessionMetadata{},
		Talks:        []domain.Talk{},
		Teams:        []domain.Team{},
		Competitions: []domain.Competition{},
	}
	var accountID pgtype.UUID
	err := q.QueryRow(ctx,
		"SELECT id, first_name, last_name, coalesce(email, ''), role, created_at, updated_at, coalesce(google_sub, '') "+
			"FROM people WHERE id = $1", pgUUID(id)).Scan(&accountID, &export.Account.FirstName, &export.Account.LastName,
		&export.Account.Email, &export.Account.Role, &export.Account.CreatedAt, &export.Account.UpdatedAt, &export.GoogleSub)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AccountExport{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AccountExport{}, translateBackendError(err)
	}
	export.Account.ID = accountID.Bytes
	if err := readExportSessions(ctx, q, id, &export); err != nil {
		return domain.AccountExport{}, err
	}
	if err := readExportTalks(ctx, q, id, &export); err != nil {
		return domain.AccountExport{}, err
	}
	if err := readExportTeams(ctx, q, id, &export); err != nil {
		return domain.AccountExport{}, err
	}
	if err := readExportCompetitions(ctx, q, id, &export); err != nil {
		return domain.AccountExport{}, err
	}
	return export, nil
}

func readExportSessions(ctx context.Context, q Querier, id uuid.UUID, export *domain.AccountExport) error {
	rows, err := q.Query(ctx,
		"SELECT created_at, expires_at FROM sessions WHERE person_id = $1 ORDER BY created_at DESC, expires_at DESC",
		pgUUID(id))
	if err != nil {
		return translateBackendError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var session domain.SessionMetadata
		if err := rows.Scan(&session.CreatedAt, &session.ExpiresAt); err != nil {
			return translateBackendError(err)
		}
		export.Sessions = append(export.Sessions, session)
	}
	return translateBackendError(rows.Err())
}

func readExportTalks(ctx context.Context, q Querier, id uuid.UUID, export *domain.AccountExport) error {
	ids, err := queryIDs(ctx, q,
		"SELECT t.id FROM talks t JOIN talk_speakers ts ON ts.talk_id = t.id "+
			"WHERE ts.person_id = $1 ORDER BY t.date DESC, t.id", pgUUID(id))
	if err != nil {
		return err
	}
	for _, talkID := range ids {
		talk, err := readTalk(ctx, q, talkID, true)
		if err != nil {
			return err
		}
		export.Talks = append(export.Talks, talk)
	}
	return nil
}

func readExportTeams(ctx context.Context, q Querier, id uuid.UUID, export *domain.AccountExport) error {
	ids, err := queryIDs(ctx, q,
		"SELECT t.id FROM teams t JOIN team_members tm ON tm.team_id = t.id "+
			"WHERE tm.person_id = $1 ORDER BY t.position NULLS LAST, t.id", pgUUID(id))
	if err != nil {
		return err
	}
	for _, teamID := range ids {
		team, err := readTeam(ctx, q, teamID)
		if err != nil {
			return err
		}
		export.Teams = append(export.Teams, team)
	}
	return nil
}

func readExportCompetitions(ctx context.Context, q Querier, id uuid.UUID, export *domain.AccountExport) error {
	ids, err := queryIDs(ctx, q,
		"SELECT DISTINCT c.id FROM competitions c JOIN teams t ON t.competition_id = c.id "+
			"JOIN team_members tm ON tm.team_id = t.id WHERE tm.person_id = $1 ORDER BY c.id", pgUUID(id))
	if err != nil {
		return err
	}
	for _, competitionID := range ids {
		competition, err := readCompetition(ctx, q, competitionID)
		if err != nil {
			return err
		}
		export.Competitions = append(export.Competitions, competition)
	}
	return nil
}

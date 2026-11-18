package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

const talkColumns = "id, title, description, date, extract(epoch from duration)::bigint, location, language, " +
	"stream_url, poster_url, published, created_at, updated_at"

const competitionColumns = "id, name, description, date, location, team_size, created_at, updated_at"

const teamColumns = "id, competition_id, name, position, photo_url, created_at, updated_at"

const userColumns = "id, first_name, last_name, email, role, created_at, updated_at"

const talkSpeakersQuery = "SELECT ts.talk_id, p.id, p.first_name, p.last_name FROM talk_speakers ts " +
	"JOIN people p ON p.id = ts.person_id WHERE ts.talk_id = ANY($1::uuid[]) ORDER BY p.last_name, p.first_name, p.id"

const teamMembersQuery = "SELECT tm.team_id, p.id, p.first_name, p.last_name FROM team_members tm " +
	"JOIN people p ON p.id = tm.person_id WHERE tm.team_id = ANY($1::uuid[]) ORDER BY p.last_name, p.first_name, p.id"

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func naiveTimestamp(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute(), value.Second(), 0, time.UTC)
}

func readTalk(ctx context.Context, q Querier, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	query := "SELECT " + talkColumns + " FROM talks WHERE id = $1"
	if !includeUnpublished {
		query += " AND published"
	}
	var talk domain.Talk
	var talkID pgtype.UUID
	var date time.Time
	err := q.QueryRow(ctx, query, pgUUID(id)).Scan(&talkID, &talk.Title, &talk.Description, &date, &talk.Duration,
		&talk.Location, &talk.Language, &talk.StreamURL, &talk.PosterURL, &talk.Published, &talk.CreatedAt, &talk.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Talk{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Talk{}, translateBackendError(err)
	}
	talk.ID = talkID.Bytes
	talk.Date = naiveTimestamp(date)
	speakers, err := readPeopleByOwner(ctx, q, talkSpeakersQuery, []uuid.UUID{id})
	if err != nil {
		return domain.Talk{}, err
	}
	talk.Speakers = speakers[id]
	return talk, nil
}

func readCompetition(ctx context.Context, q Querier, id uuid.UUID) (domain.Competition, error) {
	var competition domain.Competition
	var competitionID pgtype.UUID
	var date time.Time
	err := q.QueryRow(ctx, "SELECT "+competitionColumns+" FROM competitions WHERE id = $1", pgUUID(id)).Scan(
		&competitionID, &competition.Name, &competition.Description, &date, &competition.Location,
		&competition.TeamSize, &competition.CreatedAt, &competition.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Competition{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Competition{}, translateBackendError(err)
	}
	competition.ID = competitionID.Bytes
	competition.Date = naiveTimestamp(date)
	return competition, nil
}

func readTeam(ctx context.Context, q Querier, id uuid.UUID) (domain.Team, error) {
	teams, err := queryTeams(ctx, q, "SELECT "+teamColumns+" FROM teams WHERE id = $1", pgUUID(id))
	if err != nil {
		return domain.Team{}, err
	}
	if len(teams) == 0 {
		return domain.Team{}, domain.ErrNotFound
	}
	return teams[0], nil
}

func queryTeams(ctx context.Context, q Querier, query string, args ...any) ([]domain.Team, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, translateBackendError(err)
	}
	teams, err := scanTeamsListRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	teamIDs := make([]uuid.UUID, len(teams))
	for index := range teams {
		teamIDs[index] = teams[index].ID
	}
	members, err := readPeopleByOwner(ctx, q, teamMembersQuery, teamIDs)
	if err != nil {
		return nil, err
	}
	for index := range teams {
		teams[index].Members = members[teams[index].ID]
	}
	return teams, nil
}

func scanTeamsListRows(rows pgx.Rows) ([]domain.Team, error) {
	teams := make([]domain.Team, 0)
	for rows.Next() {
		var team domain.Team
		var teamID, competitionID pgtype.UUID
		err := rows.Scan(&teamID, &competitionID, &team.Name, &team.Position, &team.PhotoURL,
			&team.CreatedAt, &team.UpdatedAt)
		if err != nil {
			return nil, translateBackendError(err)
		}
		team.ID = teamID.Bytes
		team.CompetitionID = competitionID.Bytes
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil {
		return nil, translateBackendError(err)
	}
	return teams, nil
}

func readUser(ctx context.Context, q Querier, id uuid.UUID) (domain.User, error) {
	var user domain.User
	var userID pgtype.UUID
	err := q.QueryRow(ctx, "SELECT "+userColumns+" FROM people WHERE id = $1", pgUUID(id)).Scan(
		&userID, &user.FirstName, &user.LastName, &user.Email, &user.Role, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, translateBackendError(err)
	}
	user.ID = userID.Bytes
	return user, nil
}

func queryIDs(ctx context.Context, q Querier, query string, args ...any) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, translateBackendError(err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, translateBackendError(err)
		}
		if id.Valid {
			ids = append(ids, id.Bytes)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, translateBackendError(err)
	}
	return ids, nil
}

func readPeopleByOwner(ctx context.Context, q Querier, query string, ownerIDs []uuid.UUID) (map[uuid.UUID][]domain.Person, error) {
	owners := make(map[uuid.UUID][]domain.Person, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return owners, nil
	}
	arguments := make([]pgtype.UUID, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		owners[id] = []domain.Person{}
		arguments = append(arguments, pgUUID(id))
	}
	rows, err := q.Query(ctx, query, arguments)
	if err != nil {
		return nil, translateBackendError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID pgtype.UUID
		var person domain.Person
		var personID pgtype.UUID
		if err := rows.Scan(&ownerID, &personID, &person.FirstName, &person.LastName); err != nil {
			return nil, translateBackendError(err)
		}
		person.ID = personID.Bytes
		owners[ownerID.Bytes] = append(owners[ownerID.Bytes], person)
	}
	if err := rows.Err(); err != nil {
		return nil, translateBackendError(err)
	}
	return owners, nil
}

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
	speakerIDs, err := queryIDs(ctx, q,
		"SELECT person_id FROM talk_speakers WHERE talk_id = $1 ORDER BY person_id", pgUUID(id))
	if err != nil {
		return domain.Talk{}, err
	}
	talk.Speakers, err = readPeople(ctx, q, speakerIDs)
	if err != nil {
		return domain.Talk{}, err
	}
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
	var team domain.Team
	var teamID, competitionID pgtype.UUID
	err := q.QueryRow(ctx, "SELECT "+teamColumns+" FROM teams WHERE id = $1", pgUUID(id)).Scan(
		&teamID, &competitionID, &team.Name, &team.Position, &team.PhotoURL, &team.CreatedAt, &team.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Team{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Team{}, translateBackendError(err)
	}
	team.ID = teamID.Bytes
	team.CompetitionID = competitionID.Bytes
	memberIDs, err := queryIDs(ctx, q,
		"SELECT person_id FROM team_members WHERE team_id = $1 ORDER BY person_id", pgUUID(id))
	if err != nil {
		return domain.Team{}, err
	}
	team.Members, err = readPeople(ctx, q, memberIDs)
	if err != nil {
		return domain.Team{}, err
	}
	return team, nil
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

func readPeople(ctx context.Context, q Querier, ids []uuid.UUID) ([]domain.Person, error) {
	people := make([]domain.Person, 0, len(ids))
	if len(ids) == 0 {
		return people, nil
	}
	arguments := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		arguments = append(arguments, pgUUID(id))
	}
	rows, err := q.Query(ctx,
		"SELECT id, first_name, last_name FROM people WHERE id = ANY($1::uuid[]) ORDER BY last_name, first_name, id",
		arguments)
	if err != nil {
		return nil, translateBackendError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var person domain.Person
		var personID pgtype.UUID
		if err := rows.Scan(&personID, &person.FirstName, &person.LastName); err != nil {
			return nil, translateBackendError(err)
		}
		person.ID = personID.Bytes
		people = append(people, person)
	}
	if err := rows.Err(); err != nil {
		return nil, translateBackendError(err)
	}
	return people, nil
}

package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

const talksListUpcomingOrder = " ORDER BY date ASC, id ASC"

const talksListPastOrder = " ORDER BY date DESC, id ASC"

const talksListAllOrder = " ORDER BY (date + duration < $1::timestamp) ASC," +
	" CASE WHEN date + duration >= $1::timestamp THEN date END ASC," +
	" CASE WHEN date + duration < $1::timestamp THEN date END DESC, id ASC"

func (r *BackendRepository) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	conditions := make([]string, 0, 2)
	if !filter.IncludeUnpublished {
		conditions = append(conditions, "published")
	}
	order := talksListAllOrder
	switch filter.Period {
	case domain.TalkPeriodUpcoming:
		conditions = append(conditions, "date + duration >= $1::timestamp")
		order = talksListUpcomingOrder
	case domain.TalkPeriodPast:
		conditions = append(conditions, "date + duration < $1::timestamp")
		order = talksListPastOrder
	}
	query := "SELECT " + talkColumns + " FROM talks"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += order + " LIMIT $2 OFFSET $3"
	talks, err := queryTalks(ctx, r.db, query, naiveTimestamp(filter.Now), page.Limit, page.Offset)
	if err != nil {
		return domain.TalkPage{}, err
	}
	return domain.TalkPage{Talks: talks}, nil
}

func queryTalks(ctx context.Context, q Querier, query string, args ...any) ([]domain.Talk, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, translateBackendError(err)
	}
	talks, err := scanTalksListRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	talkIDs := make([]uuid.UUID, len(talks))
	for index := range talks {
		talkIDs[index] = talks[index].ID
	}
	speakers, err := readPeopleByOwner(ctx, q, talkSpeakersQuery, talkIDs)
	if err != nil {
		return nil, err
	}
	for index := range talks {
		talks[index].Speakers = speakers[talks[index].ID]
	}
	return talks, nil
}

func scanTalksListRows(rows pgx.Rows) ([]domain.Talk, error) {
	talks := make([]domain.Talk, 0)
	for rows.Next() {
		var talk domain.Talk
		var talkID pgtype.UUID
		var date time.Time
		err := rows.Scan(&talkID, &talk.Title, &talk.Description, &date, &talk.Duration, &talk.Location,
			&talk.Language, &talk.StreamURL, &talk.PosterURL, &talk.Published, &talk.CreatedAt, &talk.UpdatedAt)
		if err != nil {
			return nil, translateBackendError(err)
		}
		talk.ID = talkID.Bytes
		talk.Date = naiveTimestamp(date)
		talks = append(talks, talk)
	}
	if err := rows.Err(); err != nil {
		return nil, translateBackendError(err)
	}
	return talks, nil
}

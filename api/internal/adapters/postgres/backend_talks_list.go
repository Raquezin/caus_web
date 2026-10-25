package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

const talksListUpcomingOrder = " ORDER BY date ASC, id ASC"

const talksListPastOrder = " ORDER BY date DESC, id ASC"

const talksListAllOrder = " ORDER BY (date < $1::timestamp) ASC," +
	" CASE WHEN date >= $1::timestamp THEN date END ASC," +
	" CASE WHEN date < $1::timestamp THEN date END DESC, id ASC"

func (r *BackendRepository) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	conditions := make([]string, 0, 2)
	if !filter.IncludeUnpublished {
		conditions = append(conditions, "published")
	}
	order := talksListAllOrder
	switch filter.Period {
	case domain.TalkPeriodUpcoming:
		conditions = append(conditions, "date >= $1::timestamp")
		order = talksListUpcomingOrder
	case domain.TalkPeriodPast:
		conditions = append(conditions, "date < $1::timestamp")
		order = talksListPastOrder
	}
	query := "SELECT " + talkColumns + " FROM talks"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += order + " LIMIT $2 OFFSET $3"
	rows, err := r.db.Query(ctx, query, naiveTimestamp(filter.Now), page.Limit, page.Offset)
	if err != nil {
		return domain.TalkPage{}, translateBackendError(err)
	}
	talks, err := scanTalksListRows(rows)
	rows.Close()
	if err != nil {
		return domain.TalkPage{}, err
	}
	if err := hydrateTalksListSpeakers(ctx, r.db, talks); err != nil {
		return domain.TalkPage{}, err
	}
	return domain.TalkPage{Talks: talks}, nil
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

func hydrateTalksListSpeakers(ctx context.Context, q Querier, talks []domain.Talk) error {
	for index := range talks {
		ids, err := queryIDs(ctx, q, "SELECT person_id FROM talk_speakers WHERE talk_id = $1 ORDER BY person_id",
			pgUUID(talks[index].ID))
		if err != nil {
			return err
		}
		talks[index].Speakers, err = readPeople(ctx, q, ids)
		if err != nil {
			return err
		}
	}
	return nil
}

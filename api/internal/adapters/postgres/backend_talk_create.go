package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) CreateTalk(ctx context.Context, input domain.TalkInput) (domain.Talk, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Talk{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	talk, err := createTalkTx(ctx, tx, input)
	if err != nil {
		return domain.Talk{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Talk{}, err
	}
	return talk, nil
}

func createTalkTx(ctx context.Context, tx pgx.Tx, input domain.TalkInput) (domain.Talk, error) {
	if err := requireRegisteredSpeakers(ctx, tx, input.SpeakerIDs); err != nil {
		return domain.Talk{}, err
	}
	var talkID pgtype.UUID
	err := tx.QueryRow(ctx,
		"INSERT INTO talks (title, description, date, duration, location, language, stream_url, poster_url, published) "+
			"VALUES ($1, $2, $3, make_interval(secs => $4::double precision), $5, $6, $7, $8, $9) RETURNING id",
		input.Title, input.Description, naiveTimestamp(input.Date), input.Duration, input.Location, input.Language,
		input.StreamURL, input.PosterURL, input.Published).Scan(&talkID)
	if err != nil {
		return domain.Talk{}, translateBackendError(err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO talk_speakers (talk_id, person_id) SELECT $1::uuid, unnest($2::uuid[])",
		talkID, talkSpeakerArguments(input.SpeakerIDs)); err != nil {
		return domain.Talk{}, translateBackendError(err)
	}
	return readTalk(ctx, tx, talkID.Bytes, true)
}

func requireRegisteredSpeakers(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return domain.ValidationError{Fields: map[string]string{"speaker_ids": "speaker_ids must contain at least one person id"}}
	}
	var registered int
	err := tx.QueryRow(ctx,
		"SELECT count(*) FROM people WHERE id = ANY($1::uuid[]) AND email IS NOT NULL AND google_sub IS NOT NULL",
		talkSpeakerArguments(ids)).Scan(&registered)
	if err != nil {
		return translateBackendError(err)
	}
	if registered != len(ids) {
		return domain.ValidationError{Fields: map[string]string{"speaker_ids": "speaker_ids must reference registered people"}}
	}
	return nil
}

func talkSpeakerArguments(ids []uuid.UUID) []pgtype.UUID {
	arguments := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		arguments = append(arguments, pgUUID(id))
	}
	return arguments
}

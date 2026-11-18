package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func (r *BackendRepository) UpdateTalk(ctx context.Context, id uuid.UUID, patch domain.TalkPatch) (domain.Talk, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Talk{}, translateBackendError(err)
	}
	defer tx.Rollback(ctx)
	if err := lockUpdateTalk(ctx, tx, id); err != nil {
		return domain.Talk{}, err
	}
	current, err := readTalk(ctx, tx, id, true)
	if err != nil {
		return domain.Talk{}, err
	}
	merged, err := application.ApplyTalkPatch(updateTalkInputFrom(current), patch)
	if err != nil {
		return domain.Talk{}, err
	}
	if patch.SpeakerIDs != nil {
		if added := addedSpeakerIDs(current.Speakers, merged.SpeakerIDs); len(added) > 0 {
			if err := requireRegisteredSpeakers(ctx, tx, added); err != nil {
				return domain.Talk{}, err
			}
		}
	}
	if err := writeUpdateTalkScalars(ctx, tx, id, merged); err != nil {
		return domain.Talk{}, err
	}
	if patch.SpeakerIDs != nil {
		if err := replaceUpdateTalkSpeakers(ctx, tx, id, merged.SpeakerIDs); err != nil {
			return domain.Talk{}, err
		}
	}
	updated, err := readTalk(ctx, tx, id, true)
	if err != nil {
		return domain.Talk{}, err
	}
	if err := finishBackendTx(ctx, tx); err != nil {
		return domain.Talk{}, err
	}
	return updated, nil
}

func lockUpdateTalk(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	var locked pgtype.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM talks WHERE id = $1 FOR UPDATE", pgUUID(id)).Scan(&locked)
	return translateBackendError(err)
}

func updateTalkInputFrom(talk domain.Talk) domain.TalkInput {
	speakerIDs := make([]uuid.UUID, 0, len(talk.Speakers))
	for _, speaker := range talk.Speakers {
		speakerIDs = append(speakerIDs, speaker.ID)
	}
	return domain.TalkInput{
		Title:       talk.Title,
		Description: talk.Description,
		Location:    talk.Location,
		Language:    talk.Language,
		Date:        talk.Date,
		Duration:    talk.Duration,
		StreamURL:   talk.StreamURL,
		PosterURL:   talk.PosterURL,
		Published:   talk.Published,
		SpeakerIDs:  speakerIDs,
	}
}

func addedSpeakerIDs(current []domain.Person, next []uuid.UUID) []uuid.UUID {
	currentIDs := make(map[uuid.UUID]struct{}, len(current))
	for _, speaker := range current {
		currentIDs[speaker.ID] = struct{}{}
	}
	added := make([]uuid.UUID, 0, len(next))
	for _, id := range next {
		if _, ok := currentIDs[id]; !ok {
			added = append(added, id)
		}
	}
	return added
}

func writeUpdateTalkScalars(ctx context.Context, tx pgx.Tx, id uuid.UUID, input domain.TalkInput) error {
	_, err := tx.Exec(ctx,
		"UPDATE talks SET title = $2, description = $3, date = $4, duration = make_interval(secs => $5), "+
			"location = $6, language = $7, stream_url = $8, poster_url = $9, published = $10 WHERE id = $1",
		pgUUID(id), input.Title, input.Description, naiveTimestamp(input.Date), input.Duration,
		input.Location, input.Language, input.StreamURL, input.PosterURL, input.Published)
	return translateBackendError(err)
}

func replaceUpdateTalkSpeakers(ctx context.Context, tx pgx.Tx, id uuid.UUID, ids []uuid.UUID) error {
	if _, err := tx.Exec(ctx, "DELETE FROM talk_speakers WHERE talk_id = $1", pgUUID(id)); err != nil {
		return translateBackendError(err)
	}
	for _, speakerID := range ids {
		if _, err := tx.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)",
			pgUUID(id), pgUUID(speakerID)); err != nil {
			return translateBackendError(err)
		}
	}
	return nil
}

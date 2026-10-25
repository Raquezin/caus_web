package httpapi

import (
	"caus-platform/api/internal/domain"
)

type talkRequest struct {
	Title       jsonField `json:"title"`
	Description jsonField `json:"description"`
	Date        jsonField `json:"date"`
	Duration    jsonField `json:"duration"`
	Location    jsonField `json:"location"`
	Language    jsonField `json:"language"`
	StreamURL   jsonField `json:"stream_url"`
	PosterURL   jsonField `json:"poster_url"`
	Published   jsonField `json:"published"`
	SpeakerIDs  jsonField `json:"speaker_ids"`
}

func (request talkRequest) input() (domain.TalkInput, error) {
	fields := map[string]string{}
	input := domain.TalkInput{
		Title:       requiredText(request.Title, "title", fields),
		Description: requiredText(request.Description, "description", fields),
		Date:        requiredDate(request.Date, "date", fields),
		Duration:    requiredNumber[int64](request.Duration, "duration", fields),
		Location:    requiredText(request.Location, "location", fields),
		Language:    "es",
		StreamURL:   nullableText(request.StreamURL, "stream_url", fields),
		PosterURL:   nullableText(request.PosterURL, "poster_url", fields),
		SpeakerIDs:  requiredUUIDs(request.SpeakerIDs, "speaker_ids", fields),
	}
	if value, ok := strictText(request.Language, "language", fields); ok {
		input.Language = value
	}
	if value, ok := strictBool(request.Published, "published", fields); ok {
		input.Published = value
	}
	if err := invalidFields(fields); err != nil {
		return domain.TalkInput{}, err
	}
	return input, nil
}

type talkPatchRequest struct {
	Title       jsonField `json:"title"`
	Description jsonField `json:"description"`
	Date        jsonField `json:"date"`
	Duration    jsonField `json:"duration"`
	Location    jsonField `json:"location"`
	Language    jsonField `json:"language"`
	StreamURL   jsonField `json:"stream_url"`
	PosterURL   jsonField `json:"poster_url"`
	Published   jsonField `json:"published"`
	SpeakerIDs  jsonField `json:"speaker_ids"`
}

func (request talkPatchRequest) patch() (domain.TalkPatch, error) {
	fields := map[string]string{}
	patch := domain.TalkPatch{
		Title:       patchedText(request.Title, "title", fields),
		Description: patchedText(request.Description, "description", fields),
		Date:        patchedDate(request.Date, "date", fields),
		Duration:    patchedNumber[int64](request.Duration, "duration", fields),
		Location:    patchedText(request.Location, "location", fields),
		Language:    patchedText(request.Language, "language", fields),
		StreamURL:   patchedNullableText(request.StreamURL, "stream_url", fields),
		PosterURL:   patchedNullableText(request.PosterURL, "poster_url", fields),
		Published:   patchedBool(request.Published, "published", fields),
		SpeakerIDs:  patchedUUIDs(request.SpeakerIDs, "speaker_ids", fields),
	}
	if err := invalidFields(fields); err != nil {
		return domain.TalkPatch{}, err
	}
	return patch, nil
}

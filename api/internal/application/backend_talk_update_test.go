package application

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointTalkUpdateRepository struct {
	BackendRepository
	talk  domain.Talk
	err   error
	calls int
	id    uuid.UUID
	patch domain.TalkPatch
}

func (r *endpointTalkUpdateRepository) UpdateTalk(_ context.Context, id uuid.UUID, patch domain.TalkPatch) (domain.Talk, error) {
	r.calls++
	r.id = id
	r.patch = patch
	return r.talk, r.err
}

func TestEndpointTalkUpdateBackendForwardsPatch(t *testing.T) {
	repo := &endpointTalkUpdateRepository{talk: domain.Talk{ID: uuid.New(), Title: "Updated"}}
	backend := NewBackend(repo)
	id := uuid.New()
	title := "Updated"
	published := false
	stream := "https://example.com/stream"
	patch := domain.TalkPatch{
		Title:      &title,
		Published:  &published,
		StreamURL:  &domain.NullableText{Value: &stream},
		PosterURL:  &domain.NullableText{},
		SpeakerIDs: &[]uuid.UUID{uuid.New()},
	}
	talk, err := backend.UpdateTalk(context.Background(), id, patch)
	if err != nil {
		t.Fatal(err)
	}
	if talk.ID != repo.talk.ID || talk.Title != "Updated" {
		t.Fatalf("returned talk %+v", talk)
	}
	if repo.calls != 1 || repo.id != id {
		t.Fatalf("repo calls %d id %s", repo.calls, repo.id)
	}
	forwarded := repo.patch
	if forwarded.Title == nil || *forwarded.Title != title || forwarded.Published == nil || *forwarded.Published {
		t.Fatalf("forwarded patch %+v", forwarded)
	}
	if forwarded.StreamURL == nil || forwarded.StreamURL.Value == nil || *forwarded.StreamURL.Value != stream {
		t.Fatalf("forwarded stream url %+v", forwarded.StreamURL)
	}
	if forwarded.PosterURL == nil || forwarded.PosterURL.Value != nil {
		t.Fatalf("forwarded poster url %+v", forwarded.PosterURL)
	}
	if forwarded.SpeakerIDs == nil || len(*forwarded.SpeakerIDs) != 1 {
		t.Fatalf("forwarded speaker ids %+v", forwarded.SpeakerIDs)
	}
	if forwarded.Description != nil || forwarded.Date != nil || forwarded.Duration != nil {
		t.Fatalf("omitted patch fields set %+v", forwarded)
	}
}

func TestEndpointTalkUpdateBackendPropagatesErrors(t *testing.T) {
	repo := &endpointTalkUpdateRepository{err: domain.ErrNotFound}
	if _, err := NewBackend(repo).UpdateTalk(context.Background(), uuid.New(), domain.TalkPatch{}); err != domain.ErrNotFound {
		t.Fatalf("not found error %v", err)
	}
	repo.err = domain.ErrConflict
	if _, err := NewBackend(repo).UpdateTalk(context.Background(), uuid.New(), domain.TalkPatch{}); err != domain.ErrConflict {
		t.Fatalf("conflict error %v", err)
	}
	want := domain.ValidationError{Fields: map[string]string{"speaker_ids": "invalid"}}
	repo.err = want
	_, err := NewBackend(repo).UpdateTalk(context.Background(), uuid.New(), domain.TalkPatch{})
	validation, ok := err.(domain.ValidationError)
	if !ok || validation.Fields["speaker_ids"] != "invalid" {
		t.Fatalf("validation error %v", err)
	}
}

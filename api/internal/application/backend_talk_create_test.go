package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubEndpointTalkCreateRepository struct {
	BackendRepository
	calls   int
	created domain.TalkInput
	talk    domain.Talk
	err     error
}

func (s *stubEndpointTalkCreateRepository) CreateTalk(_ context.Context, input domain.TalkInput) (domain.Talk, error) {
	s.calls++
	s.created = input
	if s.err != nil {
		return domain.Talk{}, s.err
	}
	return s.talk, nil
}

func TestEndpointTalkCreateApplicationPassesValidatedInput(t *testing.T) {
	created := domain.Talk{ID: uuid.New(), Title: "Talk", Duration: 3600}
	repo := &stubEndpointTalkCreateRepository{talk: created}
	backend := NewBackend(repo)
	input := validTalkInput()
	talk, err := backend.CreateTalk(context.Background(), input)
	if err != nil {
		t.Fatalf("create talk: %v", err)
	}
	if talk.ID != created.ID {
		t.Fatalf("talk %+v, want %+v", talk, created)
	}
	if repo.calls != 1 {
		t.Fatalf("repository called %d times", repo.calls)
	}
	if repo.created.Title != input.Title || repo.created.Duration != input.Duration ||
		len(repo.created.SpeakerIDs) != 1 || repo.created.SpeakerIDs[0] != input.SpeakerIDs[0] ||
		!repo.created.Date.Equal(input.Date) || repo.created.Language != input.Language {
		t.Fatalf("repository received %+v, want %+v", repo.created, input)
	}
}

func TestEndpointTalkCreateApplicationRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(*domain.TalkInput)
	}{
		{"blank title", "title", func(in *domain.TalkInput) { in.Title = "  " }},
		{"blank description", "description", func(in *domain.TalkInput) { in.Description = "" }},
		{"blank location", "location", func(in *domain.TalkInput) { in.Location = "\t" }},
		{"unknown language", "language", func(in *domain.TalkInput) { in.Language = "fr" }},
		{"zero date", "date", func(in *domain.TalkInput) { in.Date = time.Time{} }},
		{"zero duration", "duration", func(in *domain.TalkInput) { in.Duration = 0 }},
		{"negative duration", "duration", func(in *domain.TalkInput) { in.Duration = -60 }},
		{"empty speaker ids", "speaker_ids", func(in *domain.TalkInput) { in.SpeakerIDs = nil }},
		{"duplicate speaker ids", "speaker_ids", func(in *domain.TalkInput) {
			in.SpeakerIDs = []uuid.UUID{in.SpeakerIDs[0], in.SpeakerIDs[0]}
		}},
		{"nil speaker id", "speaker_ids", func(in *domain.TalkInput) { in.SpeakerIDs = []uuid.UUID{uuid.Nil} }},
		{"invalid stream url", "stream_url", func(in *domain.TalkInput) { in.StreamURL = endpointTalkCreateText("ftp://example.com") }},
		{"invalid poster url", "poster_url", func(in *domain.TalkInput) { in.PosterURL = endpointTalkCreateText("example.com/poster") }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &stubEndpointTalkCreateRepository{}
			backend := NewBackend(repo)
			input := validTalkInput()
			testCase.mutate(&input)
			if _, err := backend.CreateTalk(context.Background(), input); err == nil {
				t.Fatal("invalid input accepted")
			} else if fields := validationFields(t, err); fields[testCase.field] == "" {
				t.Fatalf("fields %v missing %q", fields, testCase.field)
			}
			if repo.calls != 0 {
				t.Fatalf("repository called %d times for invalid input", repo.calls)
			}
		})
	}
}

func TestEndpointTalkCreateApplicationPropagatesRepositoryError(t *testing.T) {
	failure := errors.New("repository failure")
	repo := &stubEndpointTalkCreateRepository{err: failure}
	backend := NewBackend(repo)
	if _, err := backend.CreateTalk(context.Background(), validTalkInput()); !errors.Is(err, failure) {
		t.Fatalf("got %v, want repository failure", err)
	}
}

func endpointTalkCreateText(value string) *string {
	return &value
}

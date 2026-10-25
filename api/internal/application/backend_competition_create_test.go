package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubEndpointCompetitionCreateRepository struct {
	BackendRepository
	calls       int
	created     domain.CompetitionInput
	competition domain.Competition
	err         error
}

func (s *stubEndpointCompetitionCreateRepository) CreateCompetition(_ context.Context, input domain.CompetitionInput) (domain.Competition, error) {
	s.calls++
	s.created = input
	if s.err != nil {
		return domain.Competition{}, s.err
	}
	return s.competition, nil
}

func TestEndpointCompetitionCreateApplicationPassesValidatedInput(t *testing.T) {
	competition := domain.Competition{ID: uuid.New(), Name: "Spring CTF"}
	repo := &stubEndpointCompetitionCreateRepository{competition: competition}
	input := validCompetitionInput()
	got, err := NewBackend(repo).CreateCompetition(context.Background(), input)
	if err != nil {
		t.Fatalf("create competition: %v", err)
	}
	if got.ID != competition.ID {
		t.Fatalf("competition %+v, want %+v", got, competition)
	}
	if repo.calls != 1 {
		t.Fatalf("repository called %d times", repo.calls)
	}
	if repo.created.Name != input.Name || repo.created.Description != input.Description ||
		repo.created.Location != input.Location || repo.created.TeamSize != input.TeamSize ||
		!repo.created.Date.Equal(input.Date) {
		t.Fatalf("repository received %+v, want %+v", repo.created, input)
	}
	if repo.created.Date.Location() != time.UTC || repo.created.Date.Format("2006-01-02T15:04:05") != "2026-05-02T18:00:00" {
		t.Fatalf("repository date %v is not the local wall clock", repo.created.Date)
	}
}

func TestEndpointCompetitionCreateApplicationRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(*domain.CompetitionInput)
	}{
		{"blank name", "name", func(in *domain.CompetitionInput) { in.Name = "  " }},
		{"blank description", "description", func(in *domain.CompetitionInput) { in.Description = "" }},
		{"blank location", "location", func(in *domain.CompetitionInput) { in.Location = "\t" }},
		{"zero date", "date", func(in *domain.CompetitionInput) { in.Date = time.Time{} }},
		{"zero team size", "team_size", func(in *domain.CompetitionInput) { in.TeamSize = 0 }},
		{"negative team size", "team_size", func(in *domain.CompetitionInput) { in.TeamSize = -1 }},
		{"overflow team size", "team_size", func(in *domain.CompetitionInput) { in.TeamSize = 32768 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &stubEndpointCompetitionCreateRepository{}
			input := validCompetitionInput()
			testCase.mutate(&input)
			if _, err := NewBackend(repo).CreateCompetition(context.Background(), input); err == nil {
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

func TestEndpointCompetitionCreateApplicationPropagatesRepositoryError(t *testing.T) {
	failure := errors.New("repository failure")
	repo := &stubEndpointCompetitionCreateRepository{err: failure}
	if _, err := NewBackend(repo).CreateCompetition(context.Background(), validCompetitionInput()); !errors.Is(err, failure) {
		t.Fatalf("got %v, want repository failure", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repository called %d times", repo.calls)
	}
}

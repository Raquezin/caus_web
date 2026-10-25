package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type teamCreateRepository struct {
	BackendRepository
	calls         int
	ctx           context.Context
	competitionID uuid.UUID
	input         domain.TeamInput
	team          domain.Team
	err           error
}

func (s *teamCreateRepository) CreateTeam(ctx context.Context, competitionID uuid.UUID, input domain.TeamInput) (domain.Team, error) {
	s.calls++
	s.ctx = ctx
	s.competitionID = competitionID
	s.input = input
	if s.err != nil {
		return domain.Team{}, s.err
	}
	return s.team, nil
}

func teamCreateValidInput() domain.TeamInput {
	name := "Team A"
	photoURL := "https://example.com/p.png"
	position := 2
	return domain.TeamInput{
		Name:      &name,
		PhotoURL:  &photoURL,
		Position:  &position,
		MemberIDs: []uuid.UUID{uuid.New(), uuid.New()},
	}
}

type teamCreateContextKey struct{}

func TestEndpointTeamCreateApplicationPassesValidatedInput(t *testing.T) {
	competitionID := uuid.New()
	team := domain.Team{ID: uuid.New(), CompetitionID: competitionID}
	repo := &teamCreateRepository{team: team}
	ctx := context.WithValue(context.Background(), teamCreateContextKey{}, "marker")
	input := teamCreateValidInput()

	got, err := NewBackend(repo).CreateTeam(ctx, competitionID, input)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if !reflect.DeepEqual(got, team) {
		t.Fatalf("team %+v, want %+v", got, team)
	}
	if repo.calls != 1 {
		t.Fatalf("repository called %d times", repo.calls)
	}
	if repo.ctx != ctx {
		t.Fatal("repository did not receive the exact context")
	}
	if repo.competitionID != competitionID {
		t.Fatalf("competition id %s, want %s", repo.competitionID, competitionID)
	}
	if !reflect.DeepEqual(repo.input, input) {
		t.Fatalf("repository received %+v, want %+v", repo.input, input)
	}
}

func TestEndpointTeamCreateApplicationRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(*domain.TeamInput)
	}{
		{"nil members", "member_ids", func(in *domain.TeamInput) { in.MemberIDs = nil }},
		{"empty members", "member_ids", func(in *domain.TeamInput) { in.MemberIDs = []uuid.UUID{} }},
		{"duplicate members", "member_ids", func(in *domain.TeamInput) {
			in.MemberIDs = []uuid.UUID{in.MemberIDs[0], in.MemberIDs[0]}
		}},
		{"nil member", "member_ids", func(in *domain.TeamInput) { in.MemberIDs = []uuid.UUID{uuid.Nil} }},
		{"blank name", "name", func(in *domain.TeamInput) { blank := "  "; in.Name = &blank }},
		{"zero position", "position", func(in *domain.TeamInput) { zero := 0; in.Position = &zero }},
		{"invalid photo", "photo_url", func(in *domain.TeamInput) { bad := "ftp://example.com"; in.PhotoURL = &bad }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &teamCreateRepository{}
			input := teamCreateValidInput()
			testCase.mutate(&input)
			_, err := NewBackend(repo).CreateTeam(context.Background(), uuid.New(), input)
			var validation domain.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error %v is not a validation error", err)
			}
			if validation.Fields[testCase.field] == "" {
				t.Fatalf("fields %v missing %q", validation.Fields, testCase.field)
			}
			if repo.calls != 0 {
				t.Fatalf("repository called %d times for invalid input", repo.calls)
			}
		})
	}
}

func TestEndpointTeamCreateApplicationPropagatesRepositoryError(t *testing.T) {
	wrapped := errors.New("repository failure")
	cases := []struct {
		name string
		err  error
	}{
		{"not found", domain.ErrNotFound},
		{"conflict", domain.ErrConflict},
		{"wrapped", wrapped},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &teamCreateRepository{err: testCase.err}
			_, err := NewBackend(repo).CreateTeam(context.Background(), uuid.New(), teamCreateValidInput())
			if !errors.Is(err, testCase.err) {
				t.Fatalf("error %v, want %v", err, testCase.err)
			}
			if repo.calls != 1 {
				t.Fatalf("repository called %d times", repo.calls)
			}
		})
	}
}

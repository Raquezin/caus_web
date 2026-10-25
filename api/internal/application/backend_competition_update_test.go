package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type competitionUpdateRepository struct {
	BackendRepository
	competition domain.Competition
	err         error
	calls       int
	id          uuid.UUID
	patch       domain.CompetitionPatch
}

func (r *competitionUpdateRepository) UpdateCompetition(_ context.Context, id uuid.UUID, patch domain.CompetitionPatch) (domain.Competition, error) {
	r.calls++
	r.id = id
	r.patch = patch
	return r.competition, r.err
}

func TestEndpointCompetitionUpdateBackendRejectsEmptyPatch(t *testing.T) {
	repo := &competitionUpdateRepository{}
	backend := NewBackend(repo)
	_, err := backend.UpdateCompetition(context.Background(), uuid.New(), domain.CompetitionPatch{})
	if fields := validationFields(t, err); fields["patch"] == "" {
		t.Fatalf("empty patch fields %v", fields)
	}
	if repo.calls != 0 {
		t.Fatalf("empty patch reached the repository %d times", repo.calls)
	}
}

func TestEndpointCompetitionUpdateBackendRejectsInvalidFields(t *testing.T) {
	blank := "  "
	zeroDate := time.Time{}
	zeroSize := 0
	negativeSize := -1
	largeSize := maxTeamSize + 1
	cases := []struct {
		field string
		patch domain.CompetitionPatch
	}{
		{"name", domain.CompetitionPatch{Name: &blank}},
		{"description", domain.CompetitionPatch{Description: &blank}},
		{"location", domain.CompetitionPatch{Location: &blank}},
		{"date", domain.CompetitionPatch{Date: &zeroDate}},
		{"team_size", domain.CompetitionPatch{TeamSize: &zeroSize}},
		{"team_size", domain.CompetitionPatch{TeamSize: &negativeSize}},
		{"team_size", domain.CompetitionPatch{TeamSize: &largeSize}},
	}
	for _, testCase := range cases {
		repo := &competitionUpdateRepository{}
		_, err := NewBackend(repo).UpdateCompetition(context.Background(), uuid.New(), testCase.patch)
		if fields := validationFields(t, err); fields[testCase.field] == "" {
			t.Fatalf("case %s: fields %v", testCase.field, fields)
		}
		if repo.calls != 0 {
			t.Fatalf("case %s reached the repository %d times", testCase.field, repo.calls)
		}
	}
}

func TestEndpointCompetitionUpdateBackendForwardsPatchUnmerged(t *testing.T) {
	id := uuid.New()
	teamSize := 4
	result := domain.Competition{ID: id, Name: "Autumn", TeamSize: teamSize}
	repo := &competitionUpdateRepository{competition: result}
	merged, err := NewBackend(repo).UpdateCompetition(context.Background(), id, domain.CompetitionPatch{TeamSize: &teamSize})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != id || merged.TeamSize != 4 {
		t.Fatalf("result %+v", merged)
	}
	if repo.calls != 1 || repo.id != id {
		t.Fatalf("repository call %d for %v", repo.calls, repo.id)
	}
	if repo.patch.TeamSize == nil || *repo.patch.TeamSize != 4 {
		t.Fatalf("patch %+v", repo.patch)
	}
	if repo.patch.Name != nil || repo.patch.Date != nil {
		t.Fatalf("application merged omitted fields outside the lock: %+v", repo.patch)
	}
}

func TestEndpointCompetitionUpdateBackendPropagatesRepositoryErrors(t *testing.T) {
	for _, want := range []error{domain.ErrNotFound, domain.ErrConflict, errors.New("database unavailable")} {
		repo := &competitionUpdateRepository{err: want}
		name := "Autumn"
		_, err := NewBackend(repo).UpdateCompetition(context.Background(), uuid.New(), domain.CompetitionPatch{Name: &name})
		if !errors.Is(err, want) {
			t.Fatalf("error %v, want %v", err, want)
		}
		if repo.calls != 1 {
			t.Fatalf("repository called %d times", repo.calls)
		}
	}
}

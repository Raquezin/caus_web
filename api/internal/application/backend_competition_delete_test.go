package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type EndpointCompetitionDeleteRepository struct {
	BackendRepository
	id    uuid.UUID
	calls int
	err   error
}

func (r *EndpointCompetitionDeleteRepository) DeleteCompetition(_ context.Context, id uuid.UUID) error {
	r.calls++
	r.id = id
	return r.err
}

func TestEndpointCompetitionDeleteDelegates(t *testing.T) {
	repository := &EndpointCompetitionDeleteRepository{}
	id := uuid.MustParse("8f5d0a02-1b5f-4c43-9a1a-3f2f9d5b6c70")
	if err := NewBackend(repository).DeleteCompetition(context.Background(), id); err != nil {
		t.Fatalf("delete competition: %v", err)
	}
	if repository.calls != 1 || repository.id != id {
		t.Fatalf("repository calls %d for id %s, want 1 for %s", repository.calls, repository.id, id)
	}
}

func TestEndpointCompetitionDeleteReturnsRepositoryErrors(t *testing.T) {
	validation := domain.ValidationError{Fields: map[string]string{"data": "invalid value"}}
	unknown := errors.New("connection refused")
	cases := []struct {
		name  string
		err   error
		match func(error) bool
	}{
		{"not found", domain.ErrNotFound, func(err error) bool { return errors.Is(err, domain.ErrNotFound) }},
		{"conflict", domain.ErrConflict, func(err error) bool { return errors.Is(err, domain.ErrConflict) }},
		{"validation", validation, func(err error) bool {
			var found domain.ValidationError
			return errors.As(err, &found) && found.Fields["data"] == "invalid value"
		}},
		{"unknown", unknown, func(err error) bool { return errors.Is(err, unknown) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &EndpointCompetitionDeleteRepository{err: testCase.err}
			err := NewBackend(repository).DeleteCompetition(context.Background(), uuid.New())
			if !testCase.match(err) {
				t.Fatalf("error %v, want %v", err, testCase.err)
			}
			if repository.calls != 1 {
				t.Fatalf("repository calls %d, want 1", repository.calls)
			}
		})
	}
}

package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointDeleteAccountRepository struct {
	BackendRepository
	ids   []uuid.UUID
	err   error
	calls int
}

func (r *endpointDeleteAccountRepository) DeleteAccount(_ context.Context, id uuid.UUID) error {
	r.calls++
	r.ids = append(r.ids, id)
	return r.err
}

func TestEndpointDeleteAccountDelegatesSubject(t *testing.T) {
	repo := &endpointDeleteAccountRepository{}
	id := uuid.New()
	if err := NewBackend(repo).DeleteAccount(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || len(repo.ids) != 1 || repo.ids[0] != id {
		t.Fatalf("calls %d ids %v", repo.calls, repo.ids)
	}
}

func TestEndpointDeleteAccountPreservesErrors(t *testing.T) {
	wants := map[string]error{
		"missing":  domain.ErrNotFound,
		"conflict": domain.ErrConflict,
		"unknown":  errors.New("connection refused"),
	}
	for name, want := range wants {
		repo := &endpointDeleteAccountRepository{err: want}
		err := NewBackend(repo).DeleteAccount(context.Background(), uuid.New())
		if !errors.Is(err, want) {
			t.Fatalf("%s error %v, want %v", name, err, want)
		}
	}
}

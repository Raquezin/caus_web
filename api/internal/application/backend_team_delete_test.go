package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type teamDeleteContextKey struct{}

type teamDeleteRepository struct {
	BackendRepository
	gotCtx context.Context
	gotID  uuid.UUID
	err    error
	calls  int
}

func (r *teamDeleteRepository) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	r.calls++
	r.gotCtx = ctx
	r.gotID = id
	return r.err
}

func TestEndpointTeamDeleteBackendForwards(t *testing.T) {
	id := uuid.New()
	ctx := context.WithValue(context.Background(), teamDeleteContextKey{}, "marker")
	repo := &teamDeleteRepository{}
	if err := NewBackend(repo).DeleteTeam(ctx, id); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repository calls %d, want 1", repo.calls)
	}
	if repo.gotID != id {
		t.Fatalf("repository id %v, want %v", repo.gotID, id)
	}
	if repo.gotCtx != ctx {
		t.Fatal("repository did not receive the caller context")
	}
}

func TestEndpointTeamDeleteBackendForwardsEveryID(t *testing.T) {
	for _, id := range []uuid.UUID{uuid.New(), uuid.Nil, uuid.MustParse("3f6a4a3e-9d61-4f8b-b3f1-4a0f1e5c9d21")} {
		repo := &teamDeleteRepository{}
		if err := NewBackend(repo).DeleteTeam(context.Background(), id); err != nil {
			t.Fatalf("delete %v: %v", id, err)
		}
		if repo.calls != 1 || repo.gotID != id {
			t.Fatalf("id %v forwarded as %v with %d calls", id, repo.gotID, repo.calls)
		}
	}
}

func TestEndpointTeamDeleteBackendPreservesErrors(t *testing.T) {
	for name, want := range map[string]error{
		"missing":  domain.ErrNotFound,
		"conflict": domain.ErrConflict,
	} {
		repo := &teamDeleteRepository{err: want}
		if err := NewBackend(repo).DeleteTeam(context.Background(), uuid.New()); !errors.Is(err, want) {
			t.Fatalf("%s error %v, want %v", name, err, want)
		}
		if repo.calls != 1 {
			t.Fatalf("%s repository calls %d, want 1", name, repo.calls)
		}
	}
	repo := &teamDeleteRepository{err: domain.ValidationError{Fields: map[string]string{"id": "invalid"}}}
	err := NewBackend(repo).DeleteTeam(context.Background(), uuid.New())
	var validation domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["id"] != "invalid" {
		t.Fatalf("validation error %v, want field id", err)
	}
	sentinel := errors.New("connection refused")
	err = NewBackend(&teamDeleteRepository{err: sentinel}).DeleteTeam(context.Background(), uuid.New())
	if !errors.Is(err, sentinel) {
		t.Fatalf("unknown error %v, want %v", err, sentinel)
	}
}

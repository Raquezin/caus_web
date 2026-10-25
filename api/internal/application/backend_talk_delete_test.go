package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type talkDeleteContextKey struct{}

type talkDeleteRepository struct {
	BackendRepository
	err    error
	gotID  uuid.UUID
	gotCtx context.Context
	calls  int
}

func (r *talkDeleteRepository) DeleteTalk(ctx context.Context, id uuid.UUID) error {
	r.calls++
	r.gotID = id
	r.gotCtx = ctx
	return r.err
}

func TestEndpointTalkDeleteForwardsContextAndID(t *testing.T) {
	ctx := context.WithValue(context.Background(), talkDeleteContextKey{}, "marker")
	id := uuid.New()
	repository := &talkDeleteRepository{}
	if err := NewBackend(repository).DeleteTalk(ctx, id); err != nil {
		t.Fatalf("delete talk: %v", err)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls %d", repository.calls)
	}
	if repository.gotID != id {
		t.Fatalf("repository id %v, want %v", repository.gotID, id)
	}
	if repository.gotCtx != ctx {
		t.Fatal("repository did not receive the caller context")
	}
	if value := repository.gotCtx.Value(talkDeleteContextKey{}); value != "marker" {
		t.Fatalf("context value %v", value)
	}
}

func TestEndpointTalkDeletePropagatesRepositoryError(t *testing.T) {
	for _, want := range []error{domain.ErrNotFound, domain.ErrConflict, errors.New("boom")} {
		id := uuid.New()
		repository := &talkDeleteRepository{err: want}
		err := NewBackend(repository).DeleteTalk(context.Background(), id)
		if !errors.Is(err, want) {
			t.Fatalf("error %v, want %v", err, want)
		}
		if repository.calls != 1 || repository.gotID != id {
			t.Fatalf("repository id=%v calls=%d", repository.gotID, repository.calls)
		}
	}
}

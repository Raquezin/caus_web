package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type fakeRepo struct {
	gotCtx context.Context
	gotID  uuid.UUID
	person domain.Person
	err    error
	called bool
}

func (f *fakeRepo) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	f.called = true
	f.gotCtx = ctx
	f.gotID = id
	return f.person, f.err
}

func TestGetByIDPassesIDAndContext(t *testing.T) {
	repo := &fakeRepo{person: domain.Person{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}}
	svc := NewPeople(repo)
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	id := uuid.New()

	person, err := svc.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.called || repo.gotID != id {
		t.Fatalf("repo got id %v, want %v", repo.gotID, id)
	}
	if repo.gotCtx.Value(ctxKey{}) != "marker" {
		t.Fatal("context was not propagated")
	}
	if person != repo.person {
		t.Fatalf("got %+v, want %+v", person, repo.person)
	}
}

func TestGetByIDPropagatesWrappedNotFound(t *testing.T) {
	repo := &fakeRepo{err: fmt.Errorf("repo: %w", domain.ErrPersonNotFound)}
	svc := NewPeople(repo)

	_, err := svc.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrPersonNotFound) {
		t.Fatalf("got %v, want wrapped ErrPersonNotFound", err)
	}
}

func TestGetByIDPropagatesOtherErrors(t *testing.T) {
	want := errors.New("connection refused")
	repo := &fakeRepo{err: want}
	svc := NewPeople(repo)

	_, err := svc.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

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
	gotCtx    context.Context
	gotID     uuid.UUID
	gotLimit  int
	gotOffset int
	person    domain.Person
	people    []domain.Person
	err       error
	called    bool
}

func (f *fakeRepo) GetByID(ctx context.Context, id uuid.UUID) (domain.Person, error) {
	f.called = true
	f.gotCtx = ctx
	f.gotID = id
	return f.person, f.err
}

func (f *fakeRepo) List(ctx context.Context, limit, offset int) ([]domain.Person, error) {
	f.called = true
	f.gotCtx = ctx
	f.gotLimit = limit
	f.gotOffset = offset
	return f.people, f.err
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

func TestListFetchesLookaheadAndTrims(t *testing.T) {
	people := make([]domain.Person, 4)
	for i := range people {
		people[i] = domain.Person{ID: uuid.New()}
	}
	repo := &fakeRepo{people: people}
	svc := NewPeople(repo)
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	page, err := svc.List(ctx, domain.Page{Limit: 3, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotLimit != 4 || repo.gotOffset != 10 {
		t.Fatalf("repo got limit %d offset %d, want 4 10", repo.gotLimit, repo.gotOffset)
	}
	if repo.gotCtx.Value(ctxKey{}) != "marker" {
		t.Fatal("context was not propagated")
	}
	if len(page.People) != 3 || !page.HasMore {
		t.Fatalf("got %d people hasMore %v", len(page.People), page.HasMore)
	}
}

func TestListHasMoreFalseAtExactAndUnderPageSize(t *testing.T) {
	for _, n := range []int{0, 1, 3} {
		people := make([]domain.Person, n)
		repo := &fakeRepo{people: people}
		svc := NewPeople(repo)
		page, err := svc.List(context.Background(), domain.Page{Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if page.HasMore {
			t.Fatalf("n=%d: unexpected has_more", n)
		}
		if len(page.People) != n {
			t.Fatalf("n=%d: got %d people", n, len(page.People))
		}
	}
}

func TestListRejectsInvalidPageWithoutRepo(t *testing.T) {
	for _, page := range []domain.Page{
		{Limit: 0}, {Limit: -1}, {Limit: domain.MaxPageLimit + 1}, {Limit: 1, Offset: -1},
	} {
		repo := &fakeRepo{}
		svc := NewPeople(repo)
		_, err := svc.List(context.Background(), page)
		if !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v: got %v, want ErrInvalidPagination", page, err)
		}
		if repo.called {
			t.Fatalf("page %+v: repo called", page)
		}
	}
}

func TestListPropagatesRepoError(t *testing.T) {
	want := errors.New("connection refused")
	repo := &fakeRepo{err: want}
	svc := NewPeople(repo)

	_, err := svc.List(context.Background(), domain.Page{Limit: 20})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

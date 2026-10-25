package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type endpointCompetitionsListRepo struct {
	BackendRepository
	gotCtx  context.Context
	gotPage domain.Page
	result  domain.CompetitionPage
	err     error
	calls   int
}

func (r *endpointCompetitionsListRepo) ListCompetitions(ctx context.Context, page domain.Page) (domain.CompetitionPage, error) {
	r.calls++
	r.gotCtx = ctx
	r.gotPage = page
	return r.result, r.err
}

func endpointCompetitionsListRows(count int) []domain.Competition {
	competitions := make([]domain.Competition, count)
	for i := range competitions {
		competitions[i] = domain.Competition{ID: uuid.New(), Name: "Competition"}
	}
	return competitions
}

func TestEndpointCompetitionsListApplicationFetchesLookaheadAndTrims(t *testing.T) {
	repo := &endpointCompetitionsListRepo{result: domain.CompetitionPage{Competitions: endpointCompetitionsListRows(4)}}
	backend := NewBackend(repo)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")

	page, err := backend.ListCompetitions(ctx, domain.Page{Limit: 3, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotPage != (domain.Page{Limit: 4, Offset: 10}) {
		t.Fatalf("repo page %+v, want limit 4 offset 10", repo.gotPage)
	}
	if repo.gotCtx.Value(contextKey{}) != "marker" {
		t.Fatal("context was not propagated")
	}
	if len(page.Competitions) != 3 || !page.HasMore {
		t.Fatalf("got %d competitions hasMore %v, want 3 and true", len(page.Competitions), page.HasMore)
	}
	for i, competition := range page.Competitions {
		if competition.ID != repo.result.Competitions[i].ID {
			t.Fatalf("competition %d was not the first lookahead row", i)
		}
	}
}

func TestEndpointCompetitionsListApplicationOwnsHasMoreBoundary(t *testing.T) {
	for _, count := range []int{0, 2, 3} {
		repo := &endpointCompetitionsListRepo{result: domain.CompetitionPage{Competitions: endpointCompetitionsListRows(count), HasMore: true}}
		page, err := NewBackend(repo).ListCompetitions(context.Background(), domain.Page{Limit: 3})
		if err != nil {
			t.Fatalf("count %d: %v", count, err)
		}
		if page.HasMore {
			t.Fatalf("count %d: unexpected has_more", count)
		}
		if page.Competitions == nil || len(page.Competitions) != count {
			t.Fatalf("count %d: competitions %+v", count, page.Competitions)
		}
	}
	repo := &endpointCompetitionsListRepo{}
	page, err := NewBackend(repo).ListCompetitions(context.Background(), domain.Page{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Competitions == nil || len(page.Competitions) != 0 {
		t.Fatalf("nil repository slice returned as %+v, want empty slice", page.Competitions)
	}
}

func TestEndpointCompetitionsListApplicationAllowsMaxPageLimitAndRequestsLookahead(t *testing.T) {
	repo := &endpointCompetitionsListRepo{result: domain.CompetitionPage{Competitions: endpointCompetitionsListRows(domain.MaxPageLimit)}}
	page, err := NewBackend(repo).ListCompetitions(context.Background(), domain.Page{Limit: domain.MaxPageLimit, Offset: 7})
	if err != nil {
		t.Fatalf("max page limit rejected: %v", err)
	}
	if repo.gotPage != (domain.Page{Limit: domain.MaxPageLimit + 1, Offset: 7}) {
		t.Fatalf("repo page %+v, want limit %d offset 7", repo.gotPage, domain.MaxPageLimit+1)
	}
	if len(page.Competitions) != domain.MaxPageLimit || page.HasMore {
		t.Fatalf("got %d competitions hasMore %v, want %d and false", len(page.Competitions), page.HasMore, domain.MaxPageLimit)
	}
}

func TestEndpointCompetitionsListApplicationRejectsInvalidPageWithoutRepo(t *testing.T) {
	for _, page := range []domain.Page{
		{Limit: 0}, {Limit: -1}, {Limit: domain.MaxPageLimit + 1}, {Limit: 1, Offset: -1},
	} {
		repo := &endpointCompetitionsListRepo{}
		resolved, err := NewBackend(repo).ListCompetitions(context.Background(), page)
		if !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v: error %v, want invalid pagination", page, err)
		}
		if resolved.Competitions != nil || resolved.HasMore {
			t.Fatalf("page %+v: unexpected result %+v", page, resolved)
		}
		if repo.calls != 0 {
			t.Fatalf("page %+v: repository called %d times", page, repo.calls)
		}
	}
}

func TestEndpointCompetitionsListApplicationPropagatesRepoError(t *testing.T) {
	repo := &endpointCompetitionsListRepo{err: domain.ErrConflict}
	page, err := NewBackend(repo).ListCompetitions(context.Background(), domain.Page{Limit: 2})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("error %v, want conflict", err)
	}
	if page.Competitions != nil || repo.calls != 1 {
		t.Fatalf("result %+v calls %d", page, repo.calls)
	}
}

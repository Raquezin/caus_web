package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubTeamsListRepository struct {
	BackendRepository
	calls          int
	gotCtx         context.Context
	gotCompetition uuid.UUID
	gotPage        domain.Page
	result         domain.TeamPage
	err            error
}

func (r *stubTeamsListRepository) ListTeams(ctx context.Context, competitionID uuid.UUID, page domain.Page) (domain.TeamPage, error) {
	r.calls++
	r.gotCtx = ctx
	r.gotCompetition = competitionID
	r.gotPage = page
	return r.result, r.err
}

func teamsListRows(count int) []domain.Team {
	teams := make([]domain.Team, count)
	for i := range teams {
		teams[i] = domain.Team{ID: uuid.New()}
	}
	return teams
}

func TestEndpointTeamsListRejectsInvalidPageWithoutRepo(t *testing.T) {
	for _, page := range []domain.Page{
		{Limit: 0}, {Limit: -1}, {Limit: domain.MaxPageLimit + 1}, {Limit: 1, Offset: -1},
	} {
		repo := &stubTeamsListRepository{}
		resolved, err := NewBackend(repo).ListTeams(context.Background(), uuid.New(), page)
		if !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v: error %v, want invalid pagination", page, err)
		}
		if resolved.Teams != nil || resolved.HasMore {
			t.Fatalf("page %+v: unexpected result %+v", page, resolved)
		}
		if repo.calls != 0 {
			t.Fatalf("page %+v: repository called %d times", page, repo.calls)
		}
	}
	for _, page := range []domain.Page{{Limit: 1}, {Limit: domain.MaxPageLimit}} {
		repo := &stubTeamsListRepository{}
		if _, err := NewBackend(repo).ListTeams(context.Background(), uuid.New(), page); err != nil {
			t.Fatalf("page %+v: %v", page, err)
		}
		if repo.calls != 1 {
			t.Fatalf("page %+v: repository called %d times", page, repo.calls)
		}
	}
}

func TestEndpointTeamsListFetchesLookaheadAndTrims(t *testing.T) {
	competitionID := uuid.New()
	repo := &stubTeamsListRepository{result: domain.TeamPage{Teams: teamsListRows(3)}}
	backend := NewBackend(repo)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")

	page, err := backend.ListTeams(ctx, competitionID, domain.Page{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotPage != (domain.Page{Limit: 3, Offset: 4}) {
		t.Fatalf("repo page %+v, want limit 3 offset 4", repo.gotPage)
	}
	if repo.gotCompetition != competitionID {
		t.Fatalf("repo competition %v", repo.gotCompetition)
	}
	if repo.gotCtx.Value(contextKey{}) != "marker" {
		t.Fatal("context was not propagated")
	}
	if len(page.Teams) != 2 || !page.HasMore {
		t.Fatalf("got %d teams hasMore %v, want 2 and true", len(page.Teams), page.HasMore)
	}
	for i, team := range page.Teams {
		if team.ID != repo.result.Teams[i].ID {
			t.Fatalf("team %d was not the first lookahead row", i)
		}
	}
}

func TestEndpointTeamsListHasMoreBoundary(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		repo := &stubTeamsListRepository{result: domain.TeamPage{Teams: teamsListRows(count)}}
		page, err := NewBackend(repo).ListTeams(context.Background(), uuid.New(), domain.Page{Limit: 2})
		if err != nil {
			t.Fatalf("count %d: %v", count, err)
		}
		if page.HasMore {
			t.Fatalf("count %d: unexpected has_more", count)
		}
		if page.Teams == nil || len(page.Teams) != count {
			t.Fatalf("count %d: teams %+v", count, page.Teams)
		}
	}
	repo := &stubTeamsListRepository{}
	page, err := NewBackend(repo).ListTeams(context.Background(), uuid.New(), domain.Page{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Teams == nil || len(page.Teams) != 0 {
		t.Fatalf("nil repository slice returned as %+v, want empty slice", page.Teams)
	}
}

func TestEndpointTeamsListPropagatesRepoErrors(t *testing.T) {
	for _, want := range []error{domain.ErrNotFound, errors.New("boom")} {
		repo := &stubTeamsListRepository{err: want}
		page, err := NewBackend(repo).ListTeams(context.Background(), uuid.New(), domain.Page{Limit: 2})
		if !errors.Is(err, want) {
			t.Fatalf("error %v, want %v", err, want)
		}
		if page.Teams != nil || page.HasMore || repo.calls != 1 {
			t.Fatalf("result %+v calls %d", page, repo.calls)
		}
	}
}

package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type talksListContextKey struct{}

type talksListRepository struct {
	BackendRepository
	called bool
	page   domain.Page
	filter domain.TalkFilter
	marker any
	result domain.TalkPage
	err    error
}

func (r *talksListRepository) ListTalks(ctx context.Context, page domain.Page, filter domain.TalkFilter) (domain.TalkPage, error) {
	r.called = true
	r.page = page
	r.filter = filter
	r.marker = ctx.Value(talksListContextKey{})
	return r.result, r.err
}

func talksListFilter() domain.TalkFilter {
	return domain.TalkFilter{
		Period:             domain.TalkPeriodUpcoming,
		IncludeUnpublished: false,
		Now:                time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
}

func talksListTalks(count int) []domain.Talk {
	talks := make([]domain.Talk, 0, count)
	for index := range count {
		talks = append(talks, domain.Talk{
			ID:    uuid.New(),
			Title: "Talk " + string(rune('A'+index)),
			Date:  time.Date(2026, 4, 1+index, 10, 0, 0, 0, time.UTC),
		})
	}
	return talks
}

func TestEndpointTalksListApplicationRequestsOneExtraRow(t *testing.T) {
	repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(3)}}
	backend := NewBackend(repo)
	filter := talksListFilter()
	ctx := context.WithValue(context.Background(), talksListContextKey{}, "request")

	result, err := backend.ListTalks(ctx, domain.Page{Limit: 2, Offset: 5}, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.called {
		t.Fatal("repository was not called")
	}
	if repo.page != (domain.Page{Limit: 3, Offset: 5}) {
		t.Fatalf("repository page %+v, want limit 3 offset 5", repo.page)
	}
	if repo.filter != filter {
		t.Fatalf("repository filter %+v, want %+v", repo.filter, filter)
	}
	if repo.marker != "request" {
		t.Fatalf("request context not preserved: %v", repo.marker)
	}
	if len(result.Talks) != 2 || !result.HasMore {
		t.Fatalf("result %+v", result)
	}
}

func TestEndpointTalksListApplicationPassesAdminVisibility(t *testing.T) {
	repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(1)}}
	backend := NewBackend(repo)
	filter := domain.TalkFilter{
		Period:             domain.TalkPeriodAll,
		IncludeUnpublished: true,
		Now:                time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	result, err := backend.ListTalks(context.Background(), domain.Page{Limit: 20}, filter)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.filter.IncludeUnpublished || repo.filter.Period != domain.TalkPeriodAll {
		t.Fatalf("admin filter %+v", repo.filter)
	}
	if len(result.Talks) != 1 || result.HasMore {
		t.Fatalf("result %+v", result)
	}
}

func TestEndpointTalksListApplicationExactPageHasNoMore(t *testing.T) {
	repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(2)}}
	result, err := NewBackend(repo).ListTalks(context.Background(), domain.Page{Limit: 2}, talksListFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Talks) != 2 || result.HasMore {
		t.Fatalf("result %+v", result)
	}
}

func TestEndpointTalksListApplicationIgnoresRepositoryHasMoreWithoutExtraRow(t *testing.T) {
	repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(2), HasMore: true}}
	result, err := NewBackend(repo).ListTalks(context.Background(), domain.Page{Limit: 2}, talksListFilter())
	if err != nil {
		t.Fatal(err)
	}
	if repo.page.Limit != 3 {
		t.Fatalf("repository page %+v, want limit 3", repo.page)
	}
	if len(result.Talks) != 2 || result.HasMore {
		t.Fatalf("result %+v, repository has_more must not leak without an extra row", result)
	}
}

func TestEndpointTalksListApplicationMaxLimitFetchesOneMore(t *testing.T) {
	repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(101)}}
	result, err := NewBackend(repo).ListTalks(context.Background(),
		domain.Page{Limit: domain.MaxPageLimit}, talksListFilter())
	if err != nil {
		t.Fatal(err)
	}
	if domain.MaxPageLimit != 100 {
		t.Fatalf("max page limit %d, want 100", domain.MaxPageLimit)
	}
	if repo.page.Limit != 101 {
		t.Fatalf("repository page %+v, want limit 101", repo.page)
	}
	if len(result.Talks) != 100 || !result.HasMore {
		t.Fatalf("result len %d has_more %v, want 100 and true", len(result.Talks), result.HasMore)
	}
}

func TestEndpointTalksListApplicationEmptyResult(t *testing.T) {
	repo := &talksListRepository{}
	result, err := NewBackend(repo).ListTalks(context.Background(), domain.Page{Limit: 20}, talksListFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Talks) != 0 || result.HasMore {
		t.Fatalf("result %+v", result)
	}
}

func TestEndpointTalksListApplicationRejectsInvalidPage(t *testing.T) {
	for _, page := range []domain.Page{{Limit: 0}, {Limit: 101}, {Limit: 20, Offset: -1}} {
		repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(1)}}
		_, err := NewBackend(repo).ListTalks(context.Background(), page, talksListFilter())
		if !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("page %+v error %v", page, err)
		}
		if repo.called {
			t.Fatalf("page %+v reached the repository", page)
		}
	}
}

func TestEndpointTalksListApplicationRejectsInvalidPeriod(t *testing.T) {
	for _, period := range []string{"", "week", "UPCOMING"} {
		repo := &talksListRepository{result: domain.TalkPage{Talks: talksListTalks(1)}}
		filter := talksListFilter()
		filter.Period = period
		_, err := NewBackend(repo).ListTalks(context.Background(), domain.Page{Limit: 20}, filter)
		if fields := validationFields(t, err); fields["period"] == "" {
			t.Fatalf("period %q error %v", period, err)
		}
		if repo.called {
			t.Fatalf("period %q reached the repository", period)
		}
	}
}

func TestEndpointTalksListApplicationPropagatesRepositoryError(t *testing.T) {
	failure := errors.New("database unavailable")
	repo := &talksListRepository{err: failure}
	result, err := NewBackend(repo).ListTalks(context.Background(), domain.Page{Limit: 20}, talksListFilter())
	if !errors.Is(err, failure) {
		t.Fatalf("error %v", err)
	}
	if len(result.Talks) != 0 {
		t.Fatalf("result %+v", result)
	}
}

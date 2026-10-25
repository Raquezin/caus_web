package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubCompetitionGetRepository struct {
	BackendRepository
	gotCtx context.Context
	gotID  uuid.UUID
	detail domain.CompetitionDetail
	err    error
	calls  int
}

func (s *stubCompetitionGetRepository) GetCompetition(ctx context.Context, id uuid.UUID) (domain.CompetitionDetail, error) {
	s.calls++
	s.gotCtx = ctx
	s.gotID = id
	return s.detail, s.err
}

func TestEndpointCompetitionGetForwards(t *testing.T) {
	id := uuid.New()
	detail := domain.CompetitionDetail{
		Competition: domain.Competition{ID: id, Name: "Spring", TeamSize: 2},
		Teams: []domain.Team{{
			ID:            uuid.New(),
			CompetitionID: id,
			Members:       []domain.Person{{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace"}},
		}},
	}
	repo := &stubCompetitionGetRepository{detail: detail}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "marker")
	got, err := NewBackend(repo).GetCompetition(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.gotID != id {
		t.Fatalf("repository calls %d id %v", repo.calls, repo.gotID)
	}
	if repo.gotCtx.Value(contextKey{}) != "marker" {
		t.Fatal("context was not forwarded")
	}
	if !reflect.DeepEqual(got, detail) {
		t.Fatalf("got %+v, want %+v", got, detail)
	}
}

func TestEndpointCompetitionGetPropagatesError(t *testing.T) {
	repo := &stubCompetitionGetRepository{err: domain.ErrNotFound}
	if _, err := NewBackend(repo).GetCompetition(context.Background(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error %v, want not found", err)
	}
}

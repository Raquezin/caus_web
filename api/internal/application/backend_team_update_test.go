package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type teamUpdateRepository struct {
	BackendRepository
	team     domain.Team
	err      error
	gotCtx   context.Context
	gotID    uuid.UUID
	gotPatch domain.TeamPatch
	calls    int
}

func (r *teamUpdateRepository) UpdateTeam(ctx context.Context, id uuid.UUID, patch domain.TeamPatch) (domain.Team, error) {
	r.calls++
	r.gotCtx = ctx
	r.gotID = id
	r.gotPatch = patch
	return r.team, r.err
}

type teamUpdateContextKey struct{}

func TestEndpointTeamUpdateForwardsArguments(t *testing.T) {
	id := uuid.New()
	name := "Team A"
	position := 2
	member := uuid.New()
	patch := domain.TeamPatch{
		Name:      &domain.NullableText{Value: &name},
		PhotoURL:  &domain.NullableText{},
		Position:  &domain.NullableInt{Value: &position},
		MemberIDs: &[]uuid.UUID{member},
	}
	team := domain.Team{ID: id, CompetitionID: uuid.New(), Name: &name, Position: &position}
	sentinel := errors.New("repository failure")
	cases := []struct {
		name     string
		id       uuid.UUID
		patch    domain.TeamPatch
		returned domain.Team
		err      error
	}{
		{name: "success", id: id, patch: patch, returned: team},
		{name: "nil id", id: uuid.Nil, returned: team},
		{name: "error", id: id, patch: patch, err: sentinel},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &teamUpdateRepository{team: testCase.returned, err: testCase.err}
			backend := NewBackend(repo)
			ctx := context.WithValue(context.Background(), teamUpdateContextKey{}, "marker")
			got, err := backend.UpdateTeam(ctx, testCase.id, testCase.patch)
			if !errors.Is(err, testCase.err) || (testCase.err == nil && err != nil) {
				t.Fatalf("error %v, want %v", err, testCase.err)
			}
			if !reflect.DeepEqual(got, testCase.returned) {
				t.Fatalf("team %+v, want %+v", got, testCase.returned)
			}
			if repo.calls != 1 || repo.gotID != testCase.id || !reflect.DeepEqual(repo.gotPatch, testCase.patch) {
				t.Fatalf("forwarded id=%v patch=%+v calls=%d", repo.gotID, repo.gotPatch, repo.calls)
			}
			if repo.gotCtx.Value(teamUpdateContextKey{}) != "marker" {
				t.Fatalf("context value not forwarded: %v", repo.gotCtx.Value(teamUpdateContextKey{}))
			}
		})
	}
}

func TestEndpointTeamUpdateForwardsCancellation(t *testing.T) {
	repo := &teamUpdateRepository{}
	backend := NewBackend(repo)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := backend.UpdateTeam(ctx, uuid.New(), domain.TeamPatch{Name: &domain.NullableText{}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.gotCtx.Deadline(); !ok {
		t.Fatal("deadline not forwarded to repository")
	}
	if repo.gotCtx.Done() == nil {
		t.Fatal("cancellation channel not forwarded")
	}
	select {
	case <-repo.gotCtx.Done():
		t.Fatal("repository context already cancelled")
	default:
	}
	cancel()
	select {
	case <-repo.gotCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not propagate")
	}
}

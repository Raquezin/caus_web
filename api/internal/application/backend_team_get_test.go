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

type teamGetEndpointRepository struct {
	BackendRepository
	team   domain.Team
	err    error
	gotCtx context.Context
	gotID  uuid.UUID
	calls  int
}

func (r *teamGetEndpointRepository) GetTeam(ctx context.Context, id uuid.UUID) (domain.Team, error) {
	r.calls++
	r.gotCtx = ctx
	r.gotID = id
	return r.team, r.err
}

type teamGetEndpointContextKey struct{}

func TestEndpointTeamGetForwardsArguments(t *testing.T) {
	id := uuid.New()
	name := "Team Alpha"
	team := domain.Team{
		ID:            id,
		CompetitionID: uuid.New(),
		Name:          &name,
		Members: []domain.Person{{
			ID:        uuid.New(),
			FirstName: "Ada",
			LastName:  "Lovelace",
		}},
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC),
	}
	sentinel := errors.New("repository failure")
	cases := []struct {
		name     string
		id       uuid.UUID
		returned domain.Team
		err      error
	}{
		{name: "team", id: id, returned: team},
		{name: "nil id", id: uuid.Nil, returned: team},
		{name: "empty team", id: id, returned: domain.Team{}},
		{name: "error", id: id, err: sentinel},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &teamGetEndpointRepository{team: testCase.returned, err: testCase.err}
			backend := NewBackend(repo)
			ctx := context.WithValue(context.Background(), teamGetEndpointContextKey{}, "marker")
			got, err := backend.GetTeam(ctx, testCase.id)
			if testCase.err != nil {
				if err != testCase.err {
					t.Fatalf("error %v, want sentinel %v unchanged", err, testCase.err)
				}
			} else if err != nil {
				t.Fatalf("error %v, want nil", err)
			}
			if !reflect.DeepEqual(got, testCase.returned) {
				t.Fatalf("team %+v, want %+v", got, testCase.returned)
			}
			if repo.calls != 1 || repo.gotID != testCase.id {
				t.Fatalf("forwarded id=%v calls=%d", repo.gotID, repo.calls)
			}
			if repo.gotCtx.Value(teamGetEndpointContextKey{}) != "marker" {
				t.Fatalf("context value not forwarded: %v", repo.gotCtx.Value(teamGetEndpointContextKey{}))
			}
		})
	}
}

func TestEndpointTeamGetForwardsCancellation(t *testing.T) {
	repo := &teamGetEndpointRepository{}
	backend := NewBackend(repo)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), teamGetEndpointContextKey{}, "marker"), time.Minute)
	defer cancel()
	if _, err := backend.GetTeam(ctx, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if repo.gotCtx.Value(teamGetEndpointContextKey{}) != "marker" {
		t.Fatalf("context value not forwarded: %v", repo.gotCtx.Value(teamGetEndpointContextKey{}))
	}
	if _, ok := repo.gotCtx.Deadline(); !ok {
		t.Fatal("deadline not forwarded to repository")
	}
	if repo.gotCtx.Done() == nil {
		t.Fatal("cancellation channel not forwarded to repository")
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

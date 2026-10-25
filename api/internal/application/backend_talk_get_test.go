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

type talkGetRepository struct {
	BackendRepository
	talk                  domain.Talk
	err                   error
	gotCtx                context.Context
	gotID                 uuid.UUID
	gotIncludeUnpublished bool
	calls                 int
}

func (r *talkGetRepository) GetTalk(ctx context.Context, id uuid.UUID, includeUnpublished bool) (domain.Talk, error) {
	r.calls++
	r.gotCtx = ctx
	r.gotID = id
	r.gotIncludeUnpublished = includeUnpublished
	return r.talk, r.err
}

type talkGetContextKey struct{}

func TestEndpointTalkGetForwardsArguments(t *testing.T) {
	id := uuid.New()
	talk := domain.Talk{ID: id, Title: "Forwarded", Published: true}
	sentinel := errors.New("repository failure")
	cases := []struct {
		name               string
		id                 uuid.UUID
		includeUnpublished bool
		returned           domain.Talk
		err                error
	}{
		{name: "public", id: id, includeUnpublished: false, returned: talk},
		{name: "admin", id: id, includeUnpublished: true, returned: talk},
		{name: "nil id", id: uuid.Nil, includeUnpublished: false, returned: talk},
		{name: "error", id: id, includeUnpublished: false, err: sentinel},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &talkGetRepository{talk: testCase.returned, err: testCase.err}
			backend := NewBackend(repo)
			ctx := context.WithValue(context.Background(), talkGetContextKey{}, "marker")
			got, err := backend.GetTalk(ctx, testCase.id, testCase.includeUnpublished)
			if !errors.Is(err, testCase.err) || (testCase.err == nil && err != nil) {
				t.Fatalf("error %v, want %v", err, testCase.err)
			}
			if !reflect.DeepEqual(got, testCase.returned) {
				t.Fatalf("talk %+v, want %+v", got, testCase.returned)
			}
			if repo.calls != 1 || repo.gotID != testCase.id || repo.gotIncludeUnpublished != testCase.includeUnpublished {
				t.Fatalf("forwarded id=%v includeUnpublished=%v calls=%d",
					repo.gotID, repo.gotIncludeUnpublished, repo.calls)
			}
			if repo.gotCtx.Value(talkGetContextKey{}) != "marker" {
				t.Fatalf("context value not forwarded: %v", repo.gotCtx.Value(talkGetContextKey{}))
			}
		})
	}
}

func TestEndpointTalkGetForwardsCancellation(t *testing.T) {
	repo := &talkGetRepository{talk: domain.Talk{}}
	backend := NewBackend(repo)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := backend.GetTalk(ctx, uuid.New(), false); err != nil {
		t.Fatal(err)
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

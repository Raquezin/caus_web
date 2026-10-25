package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type calendarRepositoryStub struct {
	BackendRepository
	talks []domain.Talk
	err   error
	calls int
}

func (s *calendarRepositoryStub) CalendarTalks(context.Context) ([]domain.Talk, error) {
	s.calls++
	return s.talks, s.err
}

func TestEndpointCalendarApplicationDelegates(t *testing.T) {
	winter := domain.Talk{
		ID:       uuid.New(),
		Title:    "Winter",
		Date:     time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Duration: 3600,
	}
	summer := domain.Talk{
		ID:       uuid.New(),
		Title:    "Summer",
		Date:     time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Duration: 5400,
	}
	repository := &calendarRepositoryStub{talks: []domain.Talk{winter, summer}}
	backend := NewBackend(repository)
	talks, err := backend.CalendarTalks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls %d", repository.calls)
	}
	if len(talks) != 2 || talks[0].ID != winter.ID || talks[1].ID != summer.ID {
		t.Fatalf("talks %+v", talks)
	}
}

func TestEndpointCalendarApplicationPropagatesError(t *testing.T) {
	failure := errors.New("calendar repository failed")
	repository := &calendarRepositoryStub{err: failure}
	talks, err := NewBackend(repository).CalendarTalks(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("error %v", err)
	}
	if talks != nil {
		t.Fatalf("talks %+v", talks)
	}
}

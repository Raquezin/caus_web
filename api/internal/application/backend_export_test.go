package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type stubExportRepository struct {
	BackendRepository
	export domain.AccountExport
	err    error
	calls  int
	gotID  uuid.UUID
}

func (s *stubExportRepository) ExportAccount(_ context.Context, id uuid.UUID) (domain.AccountExport, error) {
	s.calls++
	s.gotID = id
	return s.export, s.err
}

func TestEndpointExportAccount(t *testing.T) {
	id := uuid.New()
	export := domain.AccountExport{
		Account:      domain.Account{ID: id, FirstName: "Ada", Email: "ada@example.com"},
		GoogleSub:    "subject-ada",
		Sessions:     []domain.SessionMetadata{},
		Talks:        []domain.Talk{},
		Teams:        []domain.Team{},
		Competitions: []domain.Competition{},
	}
	repo := &stubExportRepository{export: export}
	got, err := NewBackend(repo).ExportAccount(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.gotID != id || got.Account.ID != id || got.GoogleSub != "subject-ada" {
		t.Fatalf("export result %+v, calls %d, id %v", got, repo.calls, repo.gotID)
	}
}

func TestEndpointExportAccountRejectsNilID(t *testing.T) {
	repo := &stubExportRepository{}
	if _, err := NewBackend(repo).ExportAccount(context.Background(), uuid.Nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("nil id error %v, want not found", err)
	}
	if repo.calls != 0 {
		t.Fatalf("repository called %d times for nil id", repo.calls)
	}
}

func TestEndpointExportAccountRequiresAccount(t *testing.T) {
	repo := &stubExportRepository{export: domain.AccountExport{GoogleSub: "subject"}}
	if _, err := NewBackend(repo).ExportAccount(context.Background(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty account error %v, want not found", err)
	}
}

func TestEndpointExportAccountPropagatesRepositoryError(t *testing.T) {
	repo := &stubExportRepository{err: domain.ErrNotFound}
	if _, err := NewBackend(repo).ExportAccount(context.Background(), uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("repository error %v, want not found", err)
	}
}

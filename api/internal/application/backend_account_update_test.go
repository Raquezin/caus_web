package application

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type backendAccountUpdateRepository struct {
	BackendRepository
	account domain.Account
	err     error

	calls int
	id    uuid.UUID
	patch domain.AccountPatch
}

func (r *backendAccountUpdateRepository) UpdateAccount(_ context.Context, id uuid.UUID, patch domain.AccountPatch) (domain.Account, error) {
	r.calls++
	r.id = id
	r.patch = patch
	return r.account, r.err
}

func TestEndpointAccountUpdateRejectsEmptyPatch(t *testing.T) {
	repository := &backendAccountUpdateRepository{}
	_, err := NewBackend(repository).UpdateAccount(context.Background(), uuid.New(), domain.AccountPatch{})
	if fields := validationFields(t, err); fields["patch"] == "" {
		t.Fatalf("empty patch fields %v", fields)
	}
	if repository.calls != 0 {
		t.Fatalf("empty patch reached the repository %d times", repository.calls)
	}
}

func TestEndpointAccountUpdateRejectsInvalidNames(t *testing.T) {
	blank := "   "
	tooLong := strings.Repeat("a", maxNameLength+1)
	invalid := "Ada\x01"
	bom := "\uFEFF"
	cases := []struct {
		name  string
		patch domain.AccountPatch
		field string
	}{
		{"blank first name", domain.AccountPatch{FirstName: &blank}, "first_name"},
		{"long first name", domain.AccountPatch{FirstName: &tooLong}, "first_name"},
		{"invalid first name", domain.AccountPatch{FirstName: &invalid}, "first_name"},
		{"bom first name", domain.AccountPatch{FirstName: &bom}, "first_name"},
		{"invalid last name", domain.AccountPatch{LastName: &invalid}, "last_name"},
		{"long last name", domain.AccountPatch{LastName: &tooLong}, "last_name"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &backendAccountUpdateRepository{}
			_, err := NewBackend(repository).UpdateAccount(context.Background(), uuid.New(), testCase.patch)
			fields := validationFields(t, err)
			if fields[testCase.field] == "" {
				t.Fatalf("fields %v missing %q", fields, testCase.field)
			}
			if repository.calls != 0 {
				t.Fatalf("invalid patch reached the repository %d times", repository.calls)
			}
		})
	}
}

func TestEndpointAccountUpdateAcceptsEmptyLastName(t *testing.T) {
	empty := ""
	account := domain.Account{ID: uuid.New(), FirstName: "Ada", LastName: ""}
	repository := &backendAccountUpdateRepository{account: account}
	updated, err := NewBackend(repository).UpdateAccount(context.Background(), account.ID,
		domain.AccountPatch{LastName: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if updated != account {
		t.Fatalf("updated account %+v, want %+v", updated, account)
	}
	if repository.calls != 1 || repository.id != account.ID {
		t.Fatalf("repository call %d (%s)", repository.calls, repository.id)
	}
	if repository.patch.FirstName != nil || repository.patch.LastName == nil || *repository.patch.LastName != "" {
		t.Fatalf("forwarded patch %+v", repository.patch)
	}
}

func TestEndpointAccountUpdateForwardsValidPatch(t *testing.T) {
	firstName := "Grace"
	lastName := "Hopper"
	account := domain.Account{ID: uuid.New(), FirstName: firstName, LastName: lastName, Email: "grace@example.com"}
	repository := &backendAccountUpdateRepository{account: account}
	updated, err := NewBackend(repository).UpdateAccount(context.Background(), account.ID,
		domain.AccountPatch{FirstName: &firstName, LastName: &lastName})
	if err != nil {
		t.Fatal(err)
	}
	if updated != account {
		t.Fatalf("updated account %+v, want %+v", updated, account)
	}
	if repository.calls != 1 || repository.id != account.ID {
		t.Fatalf("repository call %d (%s)", repository.calls, repository.id)
	}
	if repository.patch.FirstName == nil || *repository.patch.FirstName != firstName ||
		repository.patch.LastName == nil || *repository.patch.LastName != lastName {
		t.Fatalf("forwarded patch %+v", repository.patch)
	}
}

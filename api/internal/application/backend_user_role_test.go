package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type backendUserRoleRepository struct {
	BackendRepository
	user domain.User
	err  error

	calls int
	id    uuid.UUID
	role  string
}

func (r *backendUserRoleRepository) UpdateUserRole(_ context.Context, id uuid.UUID, role string) (domain.User, error) {
	r.calls++
	r.id = id
	r.role = role
	return r.user, r.err
}

func TestEndpointUserRoleForwardsAllowedRoles(t *testing.T) {
	for _, role := range []string{"member", "admin"} {
		user := domain.User{ID: uuid.New(), FirstName: "Ada", LastName: "Lovelace", Role: role}
		repository := &backendUserRoleRepository{user: user}
		backend := NewBackend(repository)
		updated, err := backend.UpdateUserRole(context.Background(), user.ID, role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if updated != user {
			t.Fatalf("%s: updated user %+v, want %+v", role, updated, user)
		}
		if repository.calls != 1 || repository.id != user.ID || repository.role != role {
			t.Fatalf("%s: repository call %d (%s, %s)", role, repository.calls, repository.id, repository.role)
		}
	}
}

func TestEndpointUserRoleRejectsInvalidRoles(t *testing.T) {
	repository := &backendUserRoleRepository{}
	backend := NewBackend(repository)
	for _, role := range []string{"", "owner", "ADMIN", " admin", "member "} {
		_, err := backend.UpdateUserRole(context.Background(), uuid.New(), role)
		fields := validationFields(t, err)
		if fields["role"] == "" {
			t.Fatalf("role %q missing field error in %v", role, fields)
		}
	}
	if repository.calls != 0 {
		t.Fatalf("invalid roles reached the repository %d times", repository.calls)
	}
}

func TestEndpointUserRolePropagatesRepositoryErrors(t *testing.T) {
	missing := domain.ErrNotFound
	repository := &backendUserRoleRepository{err: missing}
	backend := NewBackend(repository)
	if _, err := backend.UpdateUserRole(context.Background(), uuid.New(), "admin"); !errors.Is(err, missing) {
		t.Fatalf("repository error %v", err)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls %d", repository.calls)
	}
}

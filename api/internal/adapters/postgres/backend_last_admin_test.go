package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func lastAdminRole(t *testing.T, q Querier, id uuid.UUID) string {
	t.Helper()
	var role string
	if err := q.QueryRow(context.Background(),
		"SELECT role::text FROM people WHERE id = $1", id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

func lastAdminTotal(t *testing.T, q Querier) int {
	t.Helper()
	var total int
	if err := q.QueryRow(context.Background(),
		"SELECT count(*) FROM people WHERE role = 'admin'").Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func TestBackendSoleAdminCannotDemoteItself(t *testing.T) {
	repo, tx := backendFixture(t)
	admin := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	if _, err := repo.UpdateUserRole(context.Background(), admin, "member"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("sole admin demotion error %v, want %v", err, domain.ErrConflict)
	}
	if role := lastAdminRole(t, tx, admin); role != "admin" {
		t.Fatalf("role after rejected demotion %q, want admin", role)
	}
}

func TestBackendSoleAdminKeepsAdminRole(t *testing.T) {
	repo, tx := backendFixture(t)
	admin := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	user, err := repo.UpdateUserRole(context.Background(), admin, "admin")
	if err != nil || user.ID != admin || user.Role != "admin" {
		t.Fatalf("sole admin kept as admin: %+v (%v)", user, err)
	}
	if role := lastAdminRole(t, tx, admin); role != "admin" {
		t.Fatalf("role after admin update %q, want admin", role)
	}
	if total := lastAdminTotal(t, tx); total != 1 {
		t.Fatalf("admin total after admin update %d, want 1", total)
	}
}

func TestBackendSoleAdminCannotDeleteAccount(t *testing.T) {
	repo, tx := backendFixture(t)
	admin := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	session := endpointDeleteAccountSession(t, tx, admin, "sole-admin-session")
	if err := repo.DeleteAccount(context.Background(), admin); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("sole admin deletion error %v, want %v", err, domain.ErrConflict)
	}
	var email, subject *string
	var role string
	if err := tx.QueryRow(context.Background(),
		"SELECT email, google_sub, role::text FROM people WHERE id = $1", admin).Scan(&email, &subject, &role); err != nil {
		t.Fatal(err)
	}
	if email == nil || *email != "ada@example.com" || subject == nil || *subject != "sub-ada" || role != "admin" {
		t.Fatalf("sole admin after rejected deletion email=%v sub=%v role=%q", email, subject, role)
	}
	if sessions := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", session); sessions != 1 {
		t.Fatalf("sole admin sessions after rejected deletion %d, want 1", sessions)
	}
}

func TestBackendTwoAdminsCanDemoteEither(t *testing.T) {
	for _, target := range []string{"first", "second"} {
		t.Run(target, func(t *testing.T) {
			repo, tx := backendFixture(t)
			first := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
			second := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "admin")
			demoted, kept := first, second
			if target == "second" {
				demoted, kept = second, first
			}
			user, err := repo.UpdateUserRole(context.Background(), demoted, "member")
			if err != nil || user.ID != demoted || user.Role != "member" {
				t.Fatalf("demote %s of two admins: %+v (%v)", target, user, err)
			}
			if role := lastAdminRole(t, tx, demoted); role != "member" {
				t.Fatalf("demoted admin role %q, want member", role)
			}
			if role := lastAdminRole(t, tx, kept); role != "admin" {
				t.Fatalf("other admin role %q, want admin", role)
			}
			if total := lastAdminTotal(t, tx); total != 1 {
				t.Fatalf("admin total after demotion %d, want 1", total)
			}
		})
	}
}

func TestBackendTwoAdminsCanDeleteEitherAccount(t *testing.T) {
	for _, target := range []string{"first", "second"} {
		t.Run(target, func(t *testing.T) {
			repo, tx := backendFixture(t)
			first := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
			second := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "admin")
			deleted, kept := first, second
			if target == "second" {
				deleted, kept = second, first
			}
			if err := repo.DeleteAccount(context.Background(), deleted); err != nil {
				t.Fatalf("delete %s of two admins: %v", target, err)
			}
			var email, subject *string
			var role string
			if err := tx.QueryRow(context.Background(),
				"SELECT email, google_sub, role::text FROM people WHERE id = $1", deleted).Scan(&email, &subject, &role); err != nil {
				t.Fatal(err)
			}
			if email != nil || subject != nil || role != "member" {
				t.Fatalf("deleted admin email=%v sub=%v role=%q", email, subject, role)
			}
			if role := lastAdminRole(t, tx, kept); role != "admin" {
				t.Fatalf("other admin role after deletion %q, want admin", role)
			}
			if total := lastAdminTotal(t, tx); total != 1 {
				t.Fatalf("admin total after deletion %d, want 1", total)
			}
		})
	}
}

func TestBackendMemberCanDeleteAccountWithoutAdmins(t *testing.T) {
	for _, admins := range []int{0, 1} {
		name := "no admins"
		if admins == 1 {
			name = "one admin"
		}
		t.Run(name, func(t *testing.T) {
			repo, tx := backendFixture(t)
			if admins == 1 {
				insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
			}
			member := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "member")
			if err := repo.DeleteAccount(context.Background(), member); err != nil {
				t.Fatalf("member deletion with %s: %v", name, err)
			}
			if role := lastAdminRole(t, tx, member); role != "member" {
				t.Fatalf("member role after deletion %q, want member", role)
			}
			if total := lastAdminTotal(t, tx); total != admins {
				t.Fatalf("admin total after member deletion %d, want %d", total, admins)
			}
		})
	}
}

func TestBackendDemoteMemberWithAnotherAdmin(t *testing.T) {
	repo, tx := backendFixture(t)
	insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	member := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "member")
	user, err := repo.UpdateUserRole(context.Background(), member, "member")
	if err != nil || user.ID != member || user.Role != "member" {
		t.Fatalf("demote member next to an admin: %+v (%v)", user, err)
	}
	if total := lastAdminTotal(t, tx); total != 1 {
		t.Fatalf("admin total after member demotion %d, want 1", total)
	}
}

func TestBackendConcurrentAdminDemotionsKeepOneAdmin(t *testing.T) {
	for round := 0; round < 10; round++ {
		_, tx := backendFixture(t)
		first := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
		second := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "admin")
		pool := endpointExportDatabase(t, tx)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		repo := NewBackendRepository(pool)
		results := make(chan error, 2)
		start := make(chan struct{})
		for _, id := range []uuid.UUID{first, second} {
			go func(id uuid.UUID) {
				<-start
				_, err := repo.UpdateUserRole(ctx, id, "member")
				results <- err
			}(id)
		}
		close(start)
		var succeeded, conflicts int
		for i := 0; i < 2; i++ {
			switch err := <-results; {
			case err == nil:
				succeeded++
			case errors.Is(err, domain.ErrConflict):
				conflicts++
			default:
				cancel()
				t.Fatalf("round %d unexpected error %v", round, err)
			}
		}
		if succeeded != 1 || conflicts != 1 {
			cancel()
			t.Fatalf("round %d demotions succeeded=%d conflicts=%d, want 1 and 1", round, succeeded, conflicts)
		}
		if total := lastAdminTotal(t, pool); total != 1 {
			cancel()
			t.Fatalf("round %d left %d admins, want 1", round, total)
		}
		cancel()
	}
}

func TestBackendDeletedAccountCannotBecomeAdmin(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	admin := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	member := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "member")
	if err := repo.DeleteAccount(ctx, member); err != nil {
		t.Fatalf("delete member account: %v", err)
	}
	_, err := repo.UpdateUserRole(ctx, member, "admin")
	var violation domain.ValidationError
	if !errors.As(err, &violation) || violation.Fields["role"] == "" {
		t.Fatalf("promote deleted account error %v, want validation on role", err)
	}
	if total := lastAdminTotal(t, tx); total != 1 {
		t.Fatalf("admin total after rejected promotion %d, want 1", total)
	}
	if err := repo.DeleteAccount(ctx, admin); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("sole admin deletion error %v, want %v", err, domain.ErrConflict)
	}
}

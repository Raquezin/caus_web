package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func markerCount(t *testing.T, tx pgx.Tx, email string) int {
	t.Helper()
	var markers int
	if err := tx.QueryRow(context.Background(),
		"SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to($1, 'UTF8'))", email).
		Scan(&markers); err != nil {
		t.Fatal(err)
	}
	return markers
}

func TestAuthBootstrapOnceAfterDeletionIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour, []string{"audit-admin@example.test"})
	ctx := context.Background()

	original, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-sub", Email: "audit-admin@example.test", FirstName: "Original", LastName: "Admin"})
	if err != nil || original.Role != "admin" {
		t.Fatalf("bootstrap admin login: %v role %q", err, original.Role)
	}
	if markers := markerCount(t, tx, "audit-admin@example.test"); markers != 1 {
		t.Fatalf("marker after bootstrap: %d", markers)
	}

	if err := repo.DeleteAccount(ctx, original.ID); err != nil {
		t.Fatalf("delete original admin: %v", err)
	}
	if markers := markerCount(t, tx, "audit-admin@example.test"); markers != 1 {
		t.Fatalf("marker removed by deletion: %d", markers)
	}

	replacement, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "new-owner-sub", Email: "audit-admin@example.test", FirstName: "New", LastName: "Owner"})
	if err != nil {
		t.Fatalf("replacement login: %v", err)
	}
	if replacement.ID == original.ID || replacement.Role != "member" {
		t.Fatalf("replacement id reused %v role %q, want new member account", replacement.ID == original.ID, replacement.Role)
	}

	reregistered, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-sub", Email: "audit-admin@example.test", FirstName: "Original", LastName: "Admin"})
	switch {
	case errors.Is(err, domain.ErrIdentityConflict):
	case err != nil:
		t.Fatalf("original re-register: %v", err)
	case reregistered.ID == original.ID || reregistered.Role != "member":
		t.Fatalf("re-registered original role %q, want member", reregistered.Role)
	}

	second, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "second-admin-sub", Email: "audit-other@example.test", FirstName: "Second", LastName: "Admin"})
	if err != nil {
		t.Fatalf("unconfigured second email: %v", err)
	}
	if second.Role != "member" {
		t.Fatalf("unconfigured email promoted to %q", second.Role)
	}
}

func TestAuthBootstrapOnceFreshEmailIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour,
		[]string{"first@example.test", "second@example.test"})
	ctx := context.Background()

	for _, identity := range []domain.GoogleIdentity{
		{EmailAuthoritative: true, Subject: "first-sub", Email: "first@example.test", FirstName: "First", LastName: "Admin"},
		{EmailAuthoritative: true, Subject: "second-sub", Email: "second@example.test", FirstName: "Second", LastName: "Admin"},
	} {
		account, _, err := auth.Login(ctx, identity)
		if err != nil || account.Role != "admin" {
			t.Fatalf("bootstrap %s: %v role %q", identity.Email, err, account.Role)
		}
	}
	var markers int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM admin_email_bindings").Scan(&markers); err != nil || markers != 2 {
		t.Fatalf("markers %d, want 2", markers)
	}
	first, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "first-sub", Email: "first@example.test", FirstName: "First", LastName: "Admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAccount(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	again, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "recycled-sub", Email: "first@example.test", FirstName: "Recycled", LastName: "Owner"})
	if err != nil || again.Role != "member" {
		t.Fatalf("consumed email re-bootstrap: %v role %q", err, again.Role)
	}
}

func TestAuthBootstrapOnceDemotionStaysMemberIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour, []string{"demote@example.test"})
	ctx := context.Background()

	original, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "demote-sub", Email: "demote@example.test", FirstName: "Demoted", LastName: "Admin"})
	if err != nil || original.Role != "admin" {
		t.Fatalf("bootstrap login: %v role %q", err, original.Role)
	}
	if _, err := tx.Exec(ctx, "UPDATE people SET role = 'member' WHERE id = $1", original.ID); err != nil {
		t.Fatal(err)
	}
	relogin, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "demote-sub", Email: "demote@example.test", FirstName: "Demoted", LastName: "Admin"})
	if err != nil || relogin.ID != original.ID || relogin.Role != "member" {
		t.Fatalf("demoted admin re-promoted: %v id %v role %q", err, relogin.ID, relogin.Role)
	}
	if markers := markerCount(t, tx, "demote@example.test"); markers != 1 {
		t.Fatalf("markers %d, want exactly 1", markers)
	}
}

func TestAuthBootstrapOnceSameSubjectRejoinIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour, []string{"rejoin-admin@example.test"})
	ctx := context.Background()

	original, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "rejoin-sub", Email: "rejoin-admin@example.test", FirstName: "Original", LastName: "Admin"})
	if err != nil || original.Role != "admin" {
		t.Fatalf("bootstrap admin login: %v role %q", err, original.Role)
	}
	if err := repo.DeleteAccount(ctx, original.ID); err != nil {
		t.Fatalf("delete original admin: %v", err)
	}
	rejoined, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "rejoin-sub", Email: "rejoin-admin@example.test", FirstName: "Original", LastName: "Admin"})
	if err != nil {
		t.Fatalf("same-subject rejoin: %v", err)
	}
	if rejoined.ID == original.ID || rejoined.Role != "member" {
		t.Fatalf("rejoined id reused %v role %q, want new member account", rejoined.ID == original.ID, rejoined.Role)
	}
	again, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "rejoin-sub", Email: "rejoin-admin@example.test", FirstName: "Original", LastName: "Admin"})
	if err != nil || again.ID != rejoined.ID || again.Role != "member" {
		t.Fatalf("repeat rejoin: %v id changed %v role %q", err, again.ID != rejoined.ID, again.Role)
	}
	if markers := markerCount(t, tx, "rejoin-admin@example.test"); markers != 1 {
		t.Fatalf("markers after consumed rejoin: %d", markers)
	}
}

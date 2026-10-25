package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func TestAuthRepositoryAdminEmailBindingIntegration(t *testing.T) {
	repo, tx := authRepositoryFixture(t)
	auth := application.NewAuth(repo, time.Hour, []string{"admin@example.com"})
	ctx := context.Background()
	first, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-admin", Email: "admin@example.com", FirstName: "Original", LastName: "Admin"})
	if err != nil || first.Role != "admin" {
		t.Fatalf("bootstrap admin login: %v role %q", err, first.Role)
	}
	if markers := markerCount(t, tx, "admin@example.com"); markers != 1 {
		t.Fatalf("bootstrap marker count %d", markers)
	}
	repeat, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-admin", Email: "admin@example.com", FirstName: "Original", LastName: "Admin"})
	if err != nil || repeat.ID != first.ID || repeat.Role != "admin" {
		t.Fatalf("bound subject repeat login: %v", err)
	}
	moved, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-admin", Email: "moved@example.com", FirstName: "Original", LastName: "Admin"})
	if err != nil || moved.ID != first.ID || moved.Role != "admin" {
		t.Fatalf("same-subject email change: %v", err)
	}
	recycled, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "new-owner", Email: "admin@example.com", FirstName: "New", LastName: "Owner"})
	if err != nil || recycled.ID == first.ID || recycled.Role != "member" {
		t.Fatalf("recycled admin address: %v id match %v role %q", err, recycled.ID == first.ID, recycled.Role)
	}
	if markers := markerCount(t, tx, "admin@example.com"); markers != 1 {
		t.Fatal("marker reassigned to recycled address owner")
	}
	if _, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-admin", Email: "admin@example.com", FirstName: "Original", LastName: "Admin"}); !errors.Is(err, domain.ErrIdentityConflict) {
		t.Fatalf("bound subject reclaiming recycled address: %v", err)
	}
	persisted, _, err := application.NewAuth(repo, time.Hour, nil).Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "original-admin", Email: "moved@example.com", FirstName: "Original", LastName: "Admin"})
	if err != nil || persisted.ID != first.ID || persisted.Role != "admin" {
		t.Fatalf("removing allowlist demoted persisted admin: %v role %q", err, persisted.Role)
	}
}

func TestAuthRepositoryBindingRollbackAndConflictIntegration(t *testing.T) {
	repo, tx := authRepositoryFixture(t)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	var holder uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO people (first_name, last_name) VALUES ('Holder', 'Person') RETURNING id").Scan(&holder); err != nil {
		t.Fatal(err)
	}
	stale := sha256.Sum256([]byte("stale-session"))
	if _, err := tx.Exec(ctx,
		"INSERT INTO sessions (token_hash, person_id, expires_at) VALUES ($1, $2, $3)", stale[:], holder, expires); err != nil {
		t.Fatal(err)
	}
	identity := domain.GoogleIdentity{EmailAuthoritative: true, Subject: "rollback-sub", Email: "rollback-admin@example.com", FirstName: "Rollback", LastName: "Person"}
	if _, err := repo.CompleteLogin(ctx, identity, stale[:], expires, true); err == nil {
		t.Fatal("colliding session hash unexpectedly accepted")
	}
	var people, bindings int
	if err := tx.QueryRow(ctx,
		"SELECT (SELECT count(*) FROM people WHERE google_sub = 'rollback-sub'), (SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to('rollback-admin@example.com', 'UTF8')))").
		Scan(&people, &bindings); err != nil {
		t.Fatal(err)
	}
	if people != 0 || bindings != 0 {
		t.Fatal("failed session did not roll back person and admin marker")
	}
	retry := sha256.Sum256([]byte("retry-session"))
	retried, err := repo.CompleteLogin(ctx, identity, retry[:], expires, true)
	if err != nil || retried.Role != "admin" {
		t.Fatalf("retry after rollback: %v role %q", err, retried.Role)
	}
	if markers := markerCount(t, tx, "rollback-admin@example.com"); markers != 1 {
		t.Fatalf("marker after retry: %d", markers)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO people (first_name, last_name, email) VALUES ('Unbound', 'Person', 'unbound@example.com')"); err != nil {
		t.Fatal(err)
	}
	conflict := domain.GoogleIdentity{EmailAuthoritative: true, Subject: "conflict-sub", Email: "unbound@example.com", FirstName: "Conflict", LastName: "Person"}
	conflictHash := sha256.Sum256([]byte("conflict"))
	if _, err := repo.CompleteLogin(ctx, conflict, conflictHash[:], expires, true); !errors.Is(err, domain.ErrIdentityConflict) {
		t.Fatalf("unlinked email: %v", err)
	}
	if markers := markerCount(t, tx, "unbound@example.com"); markers != 0 {
		t.Fatal("identity conflict consumed admin marker")
	}
}

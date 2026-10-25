package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func TestAdminGrantConsumedByDeleteIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour, []string{"consumed@example.test"})

	original, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "consumed-original-sub", FirstName: "Original", LastName: "Admin", Email: "consumed@example.test"})
	if err != nil || original.Role != "admin" {
		t.Fatalf("bootstrap login: %v role %q", err, original.Role)
	}
	if err := repo.DeleteAccount(ctx, original.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if count := markerCount(t, tx, "consumed@example.test"); count != 1 {
		t.Fatalf("consumed marker count %d", count)
	}
	recycled, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "consumed-recycled-sub", FirstName: "New", LastName: "Owner", Email: "consumed@example.test"})
	if err != nil || recycled.ID == original.ID || recycled.Role != "member" {
		t.Fatalf("recycled consumed address: %v id match %v role %q", err, recycled.ID == original.ID, recycled.Role)
	}
	if count := markerCount(t, tx, "consumed@example.test"); count != 1 {
		t.Fatalf("consumed marker rebound by recycled subject: count %d", count)
	}
}

func TestAdminGrantConsumedSameSubjectRejoinIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	auth := application.NewAuth(NewAuthRepository(tx), time.Hour, []string{"rejoin@example.test"})

	original, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "rejoin-sub", FirstName: "Original", LastName: "Admin", Email: "rejoin@example.test"})
	if err != nil || original.Role != "admin" {
		t.Fatalf("bootstrap login: %v role %q", err, original.Role)
	}
	if err := repo.DeleteAccount(ctx, original.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	rejoined, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "rejoin-sub", FirstName: "Original", LastName: "Admin", Email: "rejoin@example.test"})
	if err != nil || rejoined.Role != "member" {
		t.Fatalf("same subject rejoin after delete: %v role %q", err, rejoined.Role)
	}
	if rejoined.ID == original.ID {
		t.Fatal("same subject rejoin reused deleted account")
	}
	if count := markerCount(t, tx, "rejoin@example.test"); count != 1 {
		t.Fatalf("consumed marker rebound by original subject: count %d", count)
	}
}

func TestAdminGrantConsumedConstraintIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	ctx := context.Background()
	for _, size := range []int{0, 31, 33} {
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = savepoint.Exec(ctx,
			"INSERT INTO admin_email_bindings (email_hash) VALUES ($1)", make([]byte, size))
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("hash length %d: err %v, want check violation", size, err)
		}
		if err := savepoint.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	valid := make([]byte, 32)
	valid[0] = 1
	if _, err := tx.Exec(ctx,
		"INSERT INTO admin_email_bindings (email_hash) VALUES ($1)", valid); err != nil {
		t.Fatalf("32-byte digest rejected: %v", err)
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = savepoint.Exec(ctx, "INSERT INTO admin_email_bindings (email_hash) VALUES (NULL)")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23502" {
		t.Fatalf("null digest: err %v, want not-null violation", err)
	}
	if err := savepoint.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	savepoint, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = savepoint.Exec(ctx,
		"INSERT INTO admin_email_bindings (email_hash) VALUES ($1)", valid)
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate digest: err %v, want unique violation", err)
	}
	if err := savepoint.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

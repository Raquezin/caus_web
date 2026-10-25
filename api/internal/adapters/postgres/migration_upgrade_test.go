package postgres

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

func freshSchemaPool(t *testing.T, prefix string) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping fresh install test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := pgx.Identifier{prefix + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, schema
}

func psqlRootSchema(t *testing.T, schema string) ([]byte, error) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	root, err := filepath.Abs(filepath.Join(schemaDir(t), "..", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "psql", "-X",
		"--dbname="+dsn, "--file="+root)
	cmd.Env = append(os.Environ(), "PGOPTIONS=-c search_path="+schema)
	return cmd.CombinedOutput()
}

func TestSchemaFreshInstallIntegration(t *testing.T) {
	pool, schema := freshSchemaPool(t, "fresh_")
	ctx := context.Background()
	if out, err := psqlRootSchema(t, schema); err != nil {
		t.Fatalf("fresh install: %v\n%s", err, out)
	}

	for _, table := range []string{
		"people", "sessions", "admin_email_bindings", "talks",
		"talk_speakers", "competitions", "teams", "team_members",
	} {
		var present bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); err != nil || !present {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
	var columns int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'admin_email_bindings'").
		Scan(&columns); err != nil || columns != 1 {
		t.Fatalf("marker columns %d, want 1", columns)
	}
	var markers int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM admin_email_bindings").Scan(&markers); err != nil || markers != 0 {
		t.Fatalf("marker table not empty: %v count %d", err, markers)
	}

	auth := application.NewAuth(NewAuthRepository(pool), time.Hour, []string{"fresh-admin@example.test"})
	adminAccount, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "fresh-admin-sub", Email: "fresh-admin@example.test", FirstName: "Fresh", LastName: "Admin"})
	if err != nil || adminAccount.Role != "admin" {
		t.Fatalf("bootstrap login on fresh install: %v role %q", err, adminAccount.Role)
	}
	member, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "plain-sub", Email: "plain@example.test", FirstName: "Plain", LastName: "Member"})
	if err != nil || member.Role != "member" {
		t.Fatalf("unconfigured address login: %v role %q", err, member.Role)
	}
	repeat, _, err := auth.Login(ctx, domain.GoogleIdentity{EmailAuthoritative: true,
		Subject: "fresh-admin-sub", Email: "fresh-admin@example.test", FirstName: "Fresh", LastName: "Admin"})
	if err != nil || repeat.ID != adminAccount.ID || repeat.Role != "admin" {
		t.Fatalf("repeat admin login: %v id changed %v role %q", err, repeat.ID != adminAccount.ID, repeat.Role)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM admin_email_bindings").Scan(&markers); err != nil || markers != 1 {
		t.Fatalf("markers after bootstrap: %v count %d", err, markers)
	}
}

func TestSchemaFreshInstallIsAtomic(t *testing.T) {
	pool, schema := freshSchemaPool(t, "atomic_")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE sessions (sentinel boolean)"); err != nil {
		t.Fatal(err)
	}
	if out, err := psqlRootSchema(t, schema); err == nil {
		t.Fatalf("fresh install over conflicting table succeeded:\n%s", out)
	}
	var present bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('people') IS NOT NULL").Scan(&present); err != nil || present {
		t.Fatalf("aborted install left people table: %v", err)
	}
	var routines int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid WHERE n.nspname = current_schema()").
		Scan(&routines); err != nil || routines != 0 {
		t.Fatalf("aborted install left routines: %v count %d", err, routines)
	}
	var column string
	if err := pool.QueryRow(ctx,
		"SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'sessions'").
		Scan(&column); err != nil || column != "sentinel" {
		t.Fatalf("pre-existing sessions table changed: %v column %q", err, column)
	}
}

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var backendSchemaFiles = []string{
	"functions.sql", "people.sql", "auth.sql", "admin_email_bindings.sql", "talks.sql", "competitions.sql",
}

func backendFixture(t *testing.T) (*BackendRepository, pgx.Tx) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping backend repository integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	schema := pgx.Identifier{"backend_repo_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range backendSchemaFiles {
		script, err := os.ReadFile(filepath.Join(schemaDir(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return NewBackendRepository(tx), tx
}

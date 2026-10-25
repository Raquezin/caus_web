package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type barrierAuthPool struct {
	*pgxpool.Pool
	arrived chan struct{}
	release chan struct{}
}

func (p *barrierAuthPool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &barrierAuthTx{Tx: tx, arrived: p.arrived, release: p.release}, nil
}

type barrierAuthTx struct {
	pgx.Tx
	arrived chan struct{}
	release chan struct{}
}

func (t *barrierAuthTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "INSERT INTO people") {
		select {
		case t.arrived <- struct{}{}:
		default:
		}
		select {
		case <-t.release:
		case <-ctx.Done():
		}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}

func authConcurrencyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping concurrent login integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := "caus_auth_concurrency_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sanitized := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+sanitized); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+sanitized+" CASCADE"); err != nil {
			t.Errorf("drop concurrency schema %s: %v", name, err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = sanitized
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, file := range []string{"functions.sql", "people.sql", "auth.sql", "admin_email_bindings.sql"} {
		sql, err := os.ReadFile(filepath.Join(schemaDir(t), file))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
	return pool
}

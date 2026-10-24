package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/domain"
)

type errRow struct{ err error }

func (r errRow) Scan(dest ...any) error { return r.err }

type stubDB struct {
	row  pgx.Row
	sql  string
	args []any
	ctx  context.Context
}

func (s *stubDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	s.ctx = ctx
	s.sql = sql
	s.args = args
	return s.row
}

func TestGetByIDErrorWrapping(t *testing.T) {
	dbErr := errors.New("connection lost")
	repo := NewPeopleRepository(&stubDB{row: errRow{err: dbErr}})
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, dbErr) {
		t.Fatalf("got %v, want wrapped %v", err, dbErr)
	}
	if errors.Is(err, domain.ErrPersonNotFound) {
		t.Fatal("unexpected ErrPersonNotFound")
	}
}

func TestGetByIDExactQueryAndArgs(t *testing.T) {
	db := &stubDB{row: errRow{err: pgx.ErrNoRows}}
	repo := NewPeopleRepository(db)
	id := uuid.New()
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	_, _ = repo.GetByID(ctx, id)

	if db.sql != "SELECT id, first_name, last_name FROM people WHERE id = $1" {
		t.Fatalf("sql %q", db.sql)
	}
	if len(db.args) != 1 {
		t.Fatalf("args %v", db.args)
	}
	bound, ok := db.args[0].(pgtype.UUID)
	if !ok || !bound.Valid || bound.Bytes != id {
		t.Fatalf("arg %v, want valid UUID %v", db.args[0], id)
	}
	if db.ctx.Value(ctxKey{}) != "marker" {
		t.Fatal("context not forwarded")
	}
}

func TestGetByIDNoRowsMapsToNotFound(t *testing.T) {
	repo := NewPeopleRepository(&stubDB{row: errRow{err: pgx.ErrNoRows}})
	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, domain.ErrPersonNotFound) {
		t.Fatalf("got %v, want ErrPersonNotFound", err)
	}
}

func schemaDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "..", "schema")
}

func TestPeopleRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL search_path TO %s", schema)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"functions.sql", "people.sql"} {
		sql, err := os.ReadFile(filepath.Join(schemaDir(t), f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}

	id := uuid.New()
	if _, err := tx.Exec(ctx,
		"INSERT INTO people (id, first_name, last_name, email, google_sub, role) VALUES ($1, $2, $3, $4, $5, 'admin')",
		id, "Ada", "Lovelace", "ada@example.com", "sub-123"); err != nil {
		t.Fatal(err)
	}

	minimalID := uuid.New()
	if _, err := tx.Exec(ctx,
		"INSERT INTO people (id, first_name, last_name) VALUES ($1, $2, $3)",
		minimalID, "Grace", "Hopper"); err != nil {
		t.Fatal(err)
	}

	repo := NewPeopleRepository(tx)

	person, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if person.ID != id || person.FirstName != "Ada" || person.LastName != "Lovelace" {
		t.Fatalf("got %+v", person)
	}

	minimal, err := repo.GetByID(ctx, minimalID)
	if err != nil {
		t.Fatalf("nullable private columns broke lookup: %v", err)
	}
	if minimal.FirstName != "Grace" || minimal.LastName != "Hopper" {
		t.Fatalf("got %+v", minimal)
	}

	_, err = repo.GetByID(ctx, uuid.New())
	if !errors.Is(err, domain.ErrPersonNotFound) {
		t.Fatalf("got %v, want ErrPersonNotFound", err)
	}
}

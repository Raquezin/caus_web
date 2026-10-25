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
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/domain"
)

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

	wantOrder := []uuid.UUID{id, minimalID}
	if wantOrder[0].String() > wantOrder[1].String() {
		wantOrder[0], wantOrder[1] = wantOrder[1], wantOrder[0]
	}

	first, err := repo.List(ctx, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != wantOrder[0] {
		t.Fatalf("first page %+v", first)
	}

	rest, err := repo.List(ctx, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].ID != wantOrder[1] {
		t.Fatalf("remaining page %+v", rest)
	}

	all, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != wantOrder[0] || all[1].ID != wantOrder[1] {
		t.Fatalf("all %+v", all)
	}

	empty, err := repo.List(ctx, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty page, got %+v", empty)
	}
}

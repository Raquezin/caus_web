package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func schemaFixture(t *testing.T) pgx.Tx {
	t.Helper()
	_, tx := backendFixture(t)
	return tx
}

func flushConstraints(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
		t.Fatalf("flush constraints: %v", err)
	}
	if _, err := tx.Exec(context.Background(), "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		t.Fatalf("defer constraints: %v", err)
	}
}

func expectCheckViolation(t *testing.T, tx pgx.Tx) {
	t.Helper()
	_, err := tx.Exec(context.Background(), "SET CONSTRAINTS ALL IMMEDIATE")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("flush constraints: %v, want SQLSTATE 23514", err)
	}
}

func insertPerson(t *testing.T, tx pgx.Tx) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name) VALUES ('Ada', 'Lovelace') RETURNING id").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCompetition(t *testing.T, tx pgx.Tx, name string, teamSize int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO competitions (name, description, date, location, team_size) VALUES ($1, 'desc', now(), 'loc', $2) RETURNING id",
		name, teamSize).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertTeam(t *testing.T, tx pgx.Tx, competitionID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO teams (competition_id, name) VALUES ($1, $2) RETURNING id", competitionID, name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertTalk(t *testing.T, tx pgx.Tx) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO talks (title, description, date, duration, location) VALUES ($1, 'desc', now(), interval '1 hour', 'loc') RETURNING id",
		"talk "+uuid.NewString()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPeopleAllowEmptyLastNameIntegration(t *testing.T) {
	tx := schemaFixture(t)
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO people (first_name, last_name) VALUES ('Grace', '')"); err != nil {
		t.Fatalf("empty last name rejected: %v", err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM people WHERE last_name = ''").Scan(&count); err != nil || count != 1 {
		t.Fatalf("stored empty last name count %d: %v", count, err)
	}
}

func TestPeopleRejectBlankFirstNameIntegration(t *testing.T) {
	tx := schemaFixture(t)
	_, err := tx.Exec(context.Background(),
		"INSERT INTO people (first_name, last_name) VALUES ('  ', 'Hopper')")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("blank first name error %v, want SQLSTATE 23514", err)
	}
}

func waitForLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx,
			"SELECT coalesce(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1", pid).Scan(&waiting); err != nil {
			t.Fatalf("poll pg_stat_activity: %v", err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend %d never blocked on a lock", pid)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

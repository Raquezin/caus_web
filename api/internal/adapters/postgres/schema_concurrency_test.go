package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestCompetitionShrinkAndRegistrationIntegration(t *testing.T) {
	pool := authConcurrencyPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sql, err := os.ReadFile(filepath.Join(schemaDir(t), "competitions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply competitions.sql: %v", err)
	}
	const competitionID = "20000000-0000-0000-0000-000000000001"
	const teamID = "30000000-0000-0000-0000-000000000001"
	if _, err := pool.Exec(ctx,
		"INSERT INTO competitions (id,name,description,date,location,team_size) VALUES ($1,'Audit','Audit','2026-10-24 12:00','Audit',3)",
		competitionID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	t.Run("join waits for shrink then fails", func(t *testing.T) {
		shrink, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = shrink.Rollback(context.Background()) }()
		if _, err := shrink.Exec(ctx, "UPDATE competitions SET team_size=1 WHERE id=$1", competitionID); err != nil {
			t.Fatal(err)
		}
		if _, err := shrink.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		join, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = join.Rollback(context.Background()) }()
		var joinPID int
		if err := join.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&joinPID); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx,
			"INSERT INTO people (id,first_name,last_name) VALUES "+
				"('40000000-0000-0000-0000-000000000001','First','Person'),"+
				"('40000000-0000-0000-0000-000000000002','Second','Person'),"+
				"('40000000-0000-0000-0000-000000000003','Third','Person')"); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx, "INSERT INTO teams (id,competition_id) VALUES ($1,$2)", teamID, competitionID); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx,
			"INSERT INTO team_members (team_id,competition_id,person_id) SELECT $1,$2,id FROM people",
			teamID, competitionID); err != nil {
			t.Fatal(err)
		}
		commitErr := make(chan error, 1)
		go func() { commitErr <- join.Commit(ctx) }()
		waitForLock(t, ctx, pool, joinPID)
		if err := shrink.Commit(ctx); err != nil {
			t.Fatalf("shrink commit: %v", err)
		}
		err = <-commitErr
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("join commit error %v, want SQLSTATE 23514", err)
		}
		var teams int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM teams WHERE id=$1", teamID).Scan(&teams); err != nil {
			t.Fatal(err)
		}
		if teams != 0 {
			t.Fatalf("overfull team persisted: %d rows", teams)
		}
	})
	t.Run("shrink waits for join then fails", func(t *testing.T) {
		const competitionID = "20000000-0000-0000-0000-000000000002"
		const teamID = "30000000-0000-0000-0000-000000000002"
		if _, err := pool.Exec(ctx,
			"INSERT INTO competitions (id,name,description,date,location,team_size) VALUES ($1,'Audit2','Audit','2026-10-24 12:00','Audit',3)",
			competitionID); err != nil {
			t.Fatalf("insert competition: %v", err)
		}
		join, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = join.Rollback(context.Background()) }()
		if _, err := join.Exec(ctx,
			"INSERT INTO people (id,first_name,last_name) VALUES "+
				"('40000000-0000-0000-0000-000000000011','First','Person'),"+
				"('40000000-0000-0000-0000-000000000012','Second','Person'),"+
				"('40000000-0000-0000-0000-000000000013','Third','Person')"); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx, "INSERT INTO teams (id,competition_id) VALUES ($1,$2)", teamID, competitionID); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx,
			"INSERT INTO team_members (team_id,competition_id,person_id) SELECT $1,$2,id FROM people",
			teamID, competitionID); err != nil {
			t.Fatal(err)
		}
		if _, err := join.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil {
			t.Fatal(err)
		}
		shrinkPID := make(chan int, 1)
		shrinkErr := make(chan error, 1)
		go func() {
			tx, err := pool.Begin(ctx)
			if err != nil {
				shrinkErr <- err
				return
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var pid int
			if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				shrinkErr <- err
				return
			}
			shrinkPID <- pid
			if _, err := tx.Exec(ctx, "UPDATE competitions SET team_size=1 WHERE id=$1", competitionID); err != nil {
				shrinkErr <- err
				return
			}
			shrinkErr <- tx.Commit(ctx)
		}()
		var pid int
		select {
		case pid = <-shrinkPID:
		case err := <-shrinkErr:
			t.Fatalf("shrink transaction failed early: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		waitForLock(t, ctx, pool, pid)
		if err := join.Commit(ctx); err != nil {
			t.Fatalf("join commit: %v", err)
		}
		err = <-shrinkErr
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("shrink commit error %v, want SQLSTATE 23514", err)
		}
		var teamSize, members int
		if err := pool.QueryRow(ctx,
			"SELECT c.team_size, count(m.person_id) FROM teams t "+
				"JOIN competitions c ON c.id=t.competition_id "+
				"JOIN team_members m ON m.team_id=t.id WHERE t.id=$1 GROUP BY c.team_size",
			teamID).Scan(&teamSize, &members); err != nil {
			t.Fatal(err)
		}
		if teamSize != 3 || members != 3 {
			t.Fatalf("final state team_size=%d members=%d, want 3 and 3", teamSize, members)
		}
	})
}

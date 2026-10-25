package postgres

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/domain"
)

func endpointExportDatabase(t *testing.T, tx pgx.Tx) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	var schema string
	if err := tx.QueryRow(ctx, "SHOW search_path").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit export fixture: %v", err)
	}
	config, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop export schema %s: %v", schema, err)
		}
		pool.Close()
	})
	return pool
}

type endpointExportBarrier struct {
	*pgxpool.Pool
	pause   string
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
}

func newEndpointExportBarrier(pool *pgxpool.Pool, pause string) *endpointExportBarrier {
	return &endpointExportBarrier{
		Pool:    pool,
		pause:   pause,
		arrived: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (b *endpointExportBarrier) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &endpointExportBarrierTx{Tx: tx, barrier: b}, nil
}

func (b *endpointExportBarrier) open() {
	b.once.Do(func() { close(b.release) })
}

type endpointExportBarrierTx struct {
	pgx.Tx
	barrier *endpointExportBarrier
}

func (t *endpointExportBarrierTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.barrier.pauseBefore(ctx, sql)
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *endpointExportBarrierTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.barrier.pauseBefore(ctx, sql)
	return t.Tx.Query(ctx, sql, args...)
}

func (b *endpointExportBarrier) pauseBefore(ctx context.Context, sql string) {
	if !strings.HasPrefix(sql, b.pause) {
		return
	}
	b.arrived <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
	}
}

func endpointExportWaitLocked(t *testing.T, barrier *endpointExportBarrier) {
	t.Helper()
	select {
	case <-barrier.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the export to lock the own account")
	}
}

type endpointExportResult struct {
	export domain.AccountExport
	err    error
}

func TestEndpointExportSnapshotIsolationIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	own := insertExportPerson(t, tx, "Ada", "Lovelace", "owner@example.com", "owner-sub", "admin")
	competition := insertExportCompetition(t, tx, "Spring")
	team := insertExportTeam(t, tx, competition, "Spring Team", own)
	insertExportSession(t, tx, own, created, "own-token")
	pool := endpointExportDatabase(t, tx)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	barrier := newEndpointExportBarrier(pool, "SELECT created_at, expires_at FROM sessions")
	t.Cleanup(barrier.open)
	results := make(chan endpointExportResult, 1)
	go func() {
		export, err := NewBackendRepository(barrier).ExportAccount(ctx, own)
		results <- endpointExportResult{export: export, err: err}
	}()
	endpointExportWaitLocked(t, barrier)

	if _, err := pool.Exec(ctx,
		"UPDATE sessions SET expires_at = $2 WHERE person_id = $1", own, created.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE teams SET name = 'Renamed Team' WHERE id = $1", team); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE competitions SET name = 'Renamed' WHERE id = $1", competition); err != nil {
		t.Fatal(err)
	}

	barrier.open()
	result := <-results
	if result.err != nil {
		t.Fatalf("export: %v", result.err)
	}
	if len(result.export.Sessions) != 1 || !result.export.Sessions[0].CreatedAt.Equal(created) ||
		!result.export.Sessions[0].ExpiresAt.Equal(created.Add(2*time.Hour)) {
		t.Fatalf("sessions committed after the snapshot leaked into the export: %+v", result.export.Sessions)
	}
	if len(result.export.Teams) != 1 || result.export.Teams[0].ID != team || result.export.Teams[0].Name == nil ||
		*result.export.Teams[0].Name != "Spring Team" {
		t.Fatalf("teams committed after the snapshot leaked into the export: %+v", result.export.Teams)
	}
	if len(result.export.Competitions) != 1 || result.export.Competitions[0].ID != competition ||
		result.export.Competitions[0].Name != "Spring" {
		t.Fatalf("competitions committed after the snapshot leaked into the export: %+v", result.export.Competitions)
	}
	later, err := NewBackendRepository(pool).ExportAccount(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if len(later.Sessions) != 1 || len(later.Teams) != 1 || len(later.Competitions) != 1 {
		t.Fatalf("later export lost rows: %+v %+v %+v", later.Sessions, later.Teams, later.Competitions)
	}
	if !later.Sessions[0].ExpiresAt.Equal(created.Add(10*time.Hour)) || later.Teams[0].Name == nil ||
		*later.Teams[0].Name != "Renamed Team" || later.Competitions[0].Name != "Renamed" {
		t.Fatalf("later export missed committed changes: %+v %+v %+v", later.Sessions, later.Teams, later.Competitions)
	}
}

func TestEndpointExportSerializesWithDeleteAccountIntegration(t *testing.T) {
	_, tx := backendFixture(t)
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	own := insertExportPerson(t, tx, "Ada", "Lovelace", "owner@example.com", "owner-sub", "admin")
	foreign := insertExportPerson(t, tx, "Grace", "Hopper", "other@example.com", "other-sub", "member")
	insertExportSession(t, tx, own, created, "own-token")
	insertExportSession(t, tx, foreign, created, "foreign-token")
	pool := endpointExportDatabase(t, tx)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	barrier := newEndpointExportBarrier(pool, "SELECT created_at, expires_at FROM sessions")
	t.Cleanup(barrier.open)
	results := make(chan endpointExportResult, 1)
	go func() {
		export, err := NewBackendRepository(barrier).ExportAccount(ctx, own)
		results <- endpointExportResult{export: export, err: err}
	}()
	endpointExportWaitLocked(t, barrier)

	foreignDone := make(chan error, 1)
	go func() { foreignDone <- NewBackendRepository(pool).DeleteAccount(ctx, foreign) }()
	select {
	case err := <-foreignDone:
		if err != nil {
			t.Fatalf("deleting another account during the export: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("deleting another account blocked on the own-row export lock")
	}

	ownDone := make(chan error, 1)
	go func() { ownDone <- NewBackendRepository(pool).DeleteAccount(ctx, own) }()
	select {
	case err := <-ownDone:
		t.Fatalf("DeleteAccount ran before the export released the own-row lock: %v", err)
	case <-time.After(time.Second):
	}

	barrier.open()
	result := <-results
	if result.err != nil {
		t.Fatalf("export: %v", result.err)
	}
	if result.export.Account.Email != "owner@example.com" || result.export.GoogleSub != "owner-sub" ||
		len(result.export.Sessions) != 1 {
		t.Fatalf("export did not keep the pre-delete snapshot: %+v", result.export)
	}
	if err := <-ownDone; err != nil {
		t.Fatalf("delete after export: %v", err)
	}
	anonymized, err := NewBackendRepository(pool).ExportAccount(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if anonymized.Account.Email != "" || anonymized.GoogleSub != "" || len(anonymized.Sessions) != 0 {
		t.Fatalf("export after delete still exposes the erased account: %+v", anonymized)
	}
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

type errRow struct{ err error }

func (r errRow) Scan(dest ...any) error { return r.err }

type stubDB struct {
	row      pgx.Row
	rows     pgx.Rows
	queryErr error
	sql      string
	args     []any
	ctx      context.Context
}

func (s *stubDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	s.ctx = ctx
	s.sql = sql
	s.args = args
	return s.row
}

func (s *stubDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.ctx = ctx
	s.sql = sql
	s.args = args
	return s.rows, s.queryErr
}

type stubRows struct {
	rows    [][]any
	idx     int
	err     error
	scanErr error
	closed  bool
}

func (s *stubRows) Close()                        { s.closed = true }
func (s *stubRows) Err() error                    { return s.err }
func (s *stubRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (s *stubRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (s *stubRows) Next() bool {
	if s.idx < len(s.rows) {
		s.idx++
		return true
	}
	return false
}
func (s *stubRows) Scan(dest ...any) error {
	if s.scanErr != nil {
		return s.scanErr
	}
	row := s.rows[s.idx-1]
	for i, d := range dest {
		switch p := d.(type) {
		case *pgtype.UUID:
			*p = row[i].(pgtype.UUID)
		case *string:
			*p = row[i].(string)
		default:
			return fmt.Errorf("unsupported scan dest %T", d)
		}
	}
	return nil
}
func (s *stubRows) Values() ([]any, error) { return s.rows[s.idx-1], s.err }
func (s *stubRows) RawValues() [][]byte    { return nil }
func (s *stubRows) Conn() *pgx.Conn        { return nil }
func (s *stubRows) TypeMap() *pgtype.Map   { return pgtype.NewMap() }

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

func TestListExactQueryAndArgs(t *testing.T) {
	db := &stubDB{rows: &stubRows{}}
	repo := NewPeopleRepository(db)
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

	_, err := repo.List(ctx, 21, 40)
	if err != nil {
		t.Fatal(err)
	}
	if db.sql != "SELECT id, first_name, last_name FROM people ORDER BY id LIMIT $1 OFFSET $2" {
		t.Fatalf("sql %q", db.sql)
	}
	if len(db.args) != 2 || db.args[0] != 21 || db.args[1] != 40 {
		t.Fatalf("args %v", db.args)
	}
	if db.ctx.Value(ctxKey{}) != "marker" {
		t.Fatal("context not forwarded")
	}
}

func TestListScansRowsAndCloses(t *testing.T) {
	id := uuid.New()
	rows := &stubRows{rows: [][]any{
		{pgtype.UUID{Bytes: id, Valid: true}, "Ada", "Lovelace"},
	}}
	repo := NewPeopleRepository(&stubDB{rows: rows})

	people, err := repo.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != id || people[0].FirstName != "Ada" || people[0].LastName != "Lovelace" {
		t.Fatalf("people %+v", people)
	}
	if !rows.closed {
		t.Fatal("rows not closed")
	}
}

func TestListQueryError(t *testing.T) {
	dbErr := errors.New("connection lost")
	repo := NewPeopleRepository(&stubDB{queryErr: dbErr})

	_, err := repo.List(context.Background(), 10, 0)
	if !errors.Is(err, dbErr) {
		t.Fatalf("got %v, want wrapped %v", err, dbErr)
	}
}

func TestListScanErrorClosesRows(t *testing.T) {
	scanErr := errors.New("bad column")
	rows := &stubRows{
		rows:    [][]any{{pgtype.UUID{Bytes: uuid.New(), Valid: true}, "a", "b"}},
		scanErr: scanErr,
	}
	repo := NewPeopleRepository(&stubDB{rows: rows})

	_, err := repo.List(context.Background(), 10, 0)
	if !errors.Is(err, scanErr) {
		t.Fatalf("got %v, want wrapped %v", err, scanErr)
	}
	if !rows.closed {
		t.Fatal("rows not closed")
	}
}

func TestListRowsError(t *testing.T) {
	iterErr := errors.New("cursor lost")
	rows := &stubRows{err: iterErr}
	repo := NewPeopleRepository(&stubDB{rows: rows})

	_, err := repo.List(context.Background(), 10, 0)
	if !errors.Is(err, iterErr) {
		t.Fatalf("got %v, want wrapped %v", err, iterErr)
	}
	if !rows.closed {
		t.Fatal("rows not closed")
	}
}

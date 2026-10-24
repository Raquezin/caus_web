package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/domain"
)

func authRepositoryFixture(t *testing.T) (*AuthRepository, pgx.Tx) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping auth repository integration test")
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
	schema := "auth_repo_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"functions.sql", "people.sql", "auth.sql"} {
		sql, err := os.ReadFile(filepath.Join(schemaDir(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return NewAuthRepository(tx), tx
}

func TestAuthRepositorySessionsIntegration(t *testing.T) {
	repo, tx := authRepositoryFixture(t)
	ctx := context.Background()
	hash := sha256.Sum256([]byte("repository-test-token"))
	expires := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	identity := domain.GoogleIdentity{Subject: " opaque-subject ", Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"}
	account, err := repo.CompleteLogin(ctx, identity, hash[:], expires, false)
	if err != nil || account.ID == uuid.Nil || account.Email != identity.Email || account.Role != "member" || account.CreatedAt.IsZero() || account.UpdatedAt.IsZero() {
		t.Fatalf("create account: %v", err)
	}
	found, err := repo.FindSession(ctx, hash[:], expires.Add(-time.Microsecond))
	if err != nil || found != account {
		t.Fatalf("joined session: %v", err)
	}
	for _, now := range []time.Time{expires, expires.Add(time.Microsecond)} {
		if _, err := repo.FindSession(ctx, hash[:], now); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("expired session authenticated")
		}
	}
	missing := sha256.Sum256([]byte("missing"))
	if _, err := repo.FindSession(ctx, missing[:], time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("missing session authenticated")
	}
	var storedHash []byte
	var storedExpires, created time.Time
	var subject string
	if err := tx.QueryRow(ctx, "SELECT s.token_hash, s.expires_at, s.created_at, p.google_sub FROM sessions s JOIN people p ON s.person_id = p.id").Scan(&storedHash, &storedExpires, &created, &subject); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedHash, hash[:]) || !storedExpires.Equal(expires) || !storedExpires.After(created) || subject != identity.Subject {
		t.Fatal("repository changed hash, expiry, schema creation time or opaque subject")
	}
	if _, err := tx.Exec(ctx, "UPDATE people SET google_sub = NULL WHERE id = $1", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, hash[:], time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("unlinked person authenticated")
	}
	if _, err := tx.Exec(ctx, "UPDATE people SET email = NULL WHERE id = $1", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, hash[:], time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted account authenticated")
	}
	if err := repo.DeleteSession(ctx, hash[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindSession(ctx, hash[:], time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted session still authenticated")
	}
	if err := repo.DeleteSession(ctx, hash[:]); err != nil {
		t.Fatal("hash deletion should be idempotent")
	}
}

func TestAuthRepositoryAtomicityAndUniqueConflictIntegration(t *testing.T) {
	repo, tx := authRepositoryFixture(t)
	ctx := context.Background()
	identity := domain.GoogleIdentity{Subject: "original-subject", Email: "ada@example.com", FirstName: "Original", LastName: "Person"}
	firstHash := sha256.Sum256([]byte("first"))
	expires := time.Now().Add(time.Hour)
	account, err := repo.CompleteLogin(ctx, identity, firstHash[:], expires, true)
	if err != nil {
		t.Fatal(err)
	}
	oldHash := sha256.Sum256([]byte("expired"))
	if _, err := tx.Exec(ctx, "INSERT INTO sessions (token_hash, person_id, created_at, expires_at) VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')", oldHash[:], account.ID); err != nil {
		t.Fatal(err)
	}
	changed := identity
	changed.FirstName, changed.Email = "Updated", "updated@example.com"
	if _, err := repo.CompleteLogin(ctx, changed, []byte{1}, expires, false); err == nil {
		t.Fatal("invalid hash unexpectedly accepted")
	}
	var first, email, role string
	var count int
	if err := tx.QueryRow(ctx, "SELECT first_name, email, role, (SELECT count(*) FROM sessions) FROM people WHERE id = $1", account.ID).Scan(&first, &email, &role, &count); err != nil {
		t.Fatal(err)
	}
	if first != "Original" || email != identity.Email || role != "admin" || count != 2 {
		t.Fatal("failed session insertion did not roll back profile changes and expired purge")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO people (first_name, last_name, email, google_sub) VALUES ('Other', 'Person', 'occupied@example.com', 'other-subject')"); err != nil {
		t.Fatal(err)
	}
	changed.Email = "occupied@example.com"
	nextHash := sha256.Sum256([]byte("next"))
	if _, err := repo.CompleteLogin(ctx, changed, nextHash[:], expires, false); !errors.Is(err, domain.ErrIdentityConflict) {
		t.Fatalf("unique email collision not mapped to conflict: %v", err)
	}
	if err := tx.QueryRow(ctx, "SELECT first_name, email FROM people WHERE id = $1", account.ID).Scan(&first, &email); err != nil {
		t.Fatal(err)
	}
	if first != "Original" || email != identity.Email {
		t.Fatal("unique conflict overwrote linked account")
	}
	changed.Email = "updated@example.com"
	updated, err := repo.CompleteLogin(ctx, changed, nextHash[:], expires, false)
	if err != nil || updated.ID != account.ID || updated.Role != "admin" || updated.FirstName != "Updated" {
		t.Fatalf("successful profile update: %v", err)
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE token_hash = $1", oldHash[:]).Scan(&count); err != nil || count != 0 {
		t.Fatal("successful login did not purge expired session")
	}
	if _, err := repo.CompleteLogin(ctx, identity, nextHash[:], expires, false); err == nil || errors.Is(err, domain.ErrIdentityConflict) {
		t.Fatal("session hash collision must fail atomically, not report an identity conflict")
	}
	if err := tx.QueryRow(ctx, "SELECT email FROM people WHERE id = $1", account.ID).Scan(&email); err != nil || email != changed.Email {
		t.Fatal("session collision did not roll back person update")
	}
}

type failingAuthDB struct {
	err  error
	hash []byte
	sql  string
	now  time.Time
}

func (d *failingAuthDB) Begin(context.Context) (pgx.Tx, error) { return nil, d.err }
func (d *failingAuthDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	d.sql, d.hash, d.now = sql, args[0].([]byte), args[1].(time.Time)
	return errRow{err: d.err}
}
func (d *failingAuthDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.sql, d.hash = sql, args[0].([]byte)
	return pgconn.CommandTag{}, d.err
}

func TestAuthRepositoryErrorWrappingAndBoundHashes(t *testing.T) {
	failure := errors.New("private database error")
	db := &failingAuthDB{err: failure}
	repo := NewAuthRepository(db)
	ctx := context.Background()
	hash := sha256.Sum256([]byte("token"))
	now := time.Now()
	if _, err := repo.CompleteLogin(ctx, domain.GoogleIdentity{}, hash[:], now.Add(time.Hour), false); !errors.Is(err, failure) {
		t.Fatal("begin failure not wrapped")
	}
	if _, err := repo.FindSession(ctx, hash[:], now); !errors.Is(err, failure) || !bytes.Equal(db.hash, hash[:]) ||
		!db.now.Equal(now) || !strings.Contains(db.sql, "s.token_hash = $1 AND s.expires_at > $2") {
		t.Fatal("find failure, bound hash or clock lost")
	}
	if err := repo.DeleteSession(ctx, hash[:]); !errors.Is(err, failure) || !bytes.Equal(db.hash, hash[:]) || db.sql != "DELETE FROM sessions WHERE token_hash = $1" {
		t.Fatal("delete failure or bound hash lost")
	}
}

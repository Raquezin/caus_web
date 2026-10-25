package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/domain"
)

func TestAuthRepositoryConcurrentFirstLoginIntegration(t *testing.T) {
	pool := authConcurrencyPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	expires := time.Now().Add(time.Hour)
	type outcome struct {
		account domain.Account
		err     error
	}
	race := func(first, second domain.GoogleIdentity, firstSeed, secondSeed string, promote bool) (outcome, outcome) {
		t.Helper()
		barrier := &barrierAuthPool{Pool: pool, arrived: make(chan struct{}, 2), release: make(chan struct{})}
		repo := NewAuthRepository(barrier)
		results := make(chan outcome, 2)
		login := func(identity domain.GoogleIdentity, seed string) {
			hash := sha256.Sum256([]byte(seed))
			account, err := repo.CompleteLogin(ctx, identity, hash[:], expires, promote)
			results <- outcome{account, err}
		}
		go login(first, firstSeed)
		go login(second, secondSeed)
		for i := 0; i < 2; i++ {
			select {
			case <-barrier.arrived:
			case <-time.After(15 * time.Second):
				t.Fatal("timed out waiting for concurrent inserts")
			}
		}
		close(barrier.release)
		return <-results, <-results
	}
	count := func(sql string, arg any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql, arg).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	shared := domain.GoogleIdentity{Subject: "shared-subject", Email: "shared@example.com", FirstName: "Shared", LastName: "Person"}
	a, b := race(shared, shared, "shared-first", "shared-second", false)
	if a.err != nil || b.err != nil || a.account.ID != b.account.ID {
		t.Fatalf("same-identity concurrent logins: %v / %v", a.err, b.err)
	}
	if people := count("SELECT count(*) FROM people WHERE google_sub = $1", "shared-subject"); people != 1 {
		t.Fatalf("duplicate person created: %d", people)
	}
	if sessions := count("SELECT count(*) FROM sessions s JOIN people p ON p.id = s.person_id WHERE p.google_sub = $1", "shared-subject"); sessions != 2 {
		t.Fatalf("sessions %d, want 2", sessions)
	}
	first := domain.GoogleIdentity{Subject: "changed-subject", Email: "first@example.com", FirstName: "First", LastName: "Person"}
	second := domain.GoogleIdentity{Subject: "changed-subject", Email: "second@example.com", FirstName: "Second", LastName: "Person"}
	a, b = race(first, second, "changed-first", "changed-second", false)
	if a.err != nil || b.err != nil || a.account.ID != b.account.ID {
		t.Fatalf("same-subject concurrent logins: %v / %v", a.err, b.err)
	}
	var email string
	if err := pool.QueryRow(ctx, "SELECT email FROM people WHERE google_sub = $1", "changed-subject").Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "first@example.com" && email != "second@example.com" {
		t.Fatalf("unexpected final email %q", email)
	}
	attacker := domain.GoogleIdentity{Subject: "attacker-subject", Email: "victim@example.com", FirstName: "Attacker", LastName: "Person"}
	victim := domain.GoogleIdentity{Subject: "victim-subject", Email: "victim@example.com", FirstName: "Victim", LastName: "Person"}
	a, b = race(attacker, victim, "takeover-first", "takeover-second", false)
	succeeded, conflicts := 0, 0
	for _, result := range []outcome{a, b} {
		switch {
		case result.err == nil:
			succeeded++
		case errors.Is(result.err, domain.ErrIdentityConflict):
			conflicts++
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("concurrent email collision: %d successes %d conflicts", succeeded, conflicts)
	}
	if people := count("SELECT count(*) FROM people WHERE email = $1", "victim@example.com"); people != 1 {
		t.Fatalf("people %d, want 1", people)
	}
	if sessions := count("SELECT count(*) FROM sessions s JOIN people p ON p.id = s.person_id WHERE p.email = $1", "victim@example.com"); sessions != 1 {
		t.Fatalf("sessions %d, want 1", sessions)
	}
	admin := domain.GoogleIdentity{Subject: "admin-subject", Email: "admin-bind@example.com", FirstName: "Admin", LastName: "Person"}
	a, b = race(admin, admin, "admin-first", "admin-second", true)
	if a.err != nil || b.err != nil || a.account.ID != b.account.ID || a.account.Role != "admin" || b.account.Role != "admin" {
		t.Fatalf("concurrent admin logins: %v / %v roles %q %q", a.err, b.err, a.account.Role, b.account.Role)
	}
	if markers := count("SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to($1, 'UTF8'))", "admin-bind@example.com"); markers != 1 {
		t.Fatalf("admin markers %d, want 1", markers)
	}
	if sessions := count("SELECT count(*) FROM sessions s JOIN people p ON p.id = s.person_id WHERE p.google_sub = $1", "admin-subject"); sessions != 2 {
		t.Fatalf("admin sessions %d, want 2", sessions)
	}
	adminAttacker := domain.GoogleIdentity{Subject: "admin-attacker", Email: "admin-race@example.com", FirstName: "Attacker", LastName: "Person"}
	adminVictim := domain.GoogleIdentity{Subject: "admin-victim", Email: "admin-race@example.com", FirstName: "Victim", LastName: "Person"}
	a, b = race(adminAttacker, adminVictim, "admin-takeover-first", "admin-takeover-second", true)
	succeeded, conflicts = 0, 0
	for _, result := range []outcome{a, b} {
		switch {
		case result.err == nil:
			succeeded++
		case errors.Is(result.err, domain.ErrIdentityConflict):
			conflicts++
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("concurrent admin email collision: %d successes %d conflicts", succeeded, conflicts)
	}
	if markers := count("SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to($1, 'UTF8'))", "admin-race@example.com"); markers != 1 {
		t.Fatalf("admin markers %d, want 1", markers)
	}
	for _, result := range []outcome{a, b} {
		if result.err == nil && result.account.Role != "admin" {
			t.Fatalf("winning admin login role %q", result.account.Role)
		}
	}
}

func TestAuthConcurrencyPoolCleanupIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping concurrency fixture cleanup test")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var schema string
	t.Run("fixture", func(t *testing.T) {
		pool := authConcurrencyPool(t)
		if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(schema, "caus_auth_concurrency_") {
		t.Fatalf("unexpected fixture schema %q", schema)
	}
	var exists bool
	if err := admin.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatalf("concurrency schema %s retained after fixture cleanup", schema)
	}
}

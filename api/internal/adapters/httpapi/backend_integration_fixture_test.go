package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"caus-platform/api/internal/adapters/postgres"
	"caus-platform/api/internal/application"
	"caus-platform/api/internal/domain"
)

var integrationSchemaFiles = []string{
	"functions.sql", "people.sql", "auth.sql", "admin_email_bindings.sql", "talks.sql", "competitions.sql",
}

type integrationProvider struct{}

func (integrationProvider) AuthorizationURL(context.Context, string, string, string) (string, error) {
	return "https://accounts.example.test/authorize", nil
}

func (integrationProvider) Exchange(context.Context, string, string, string) (domain.GoogleIdentity, error) {
	return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
}

type safeLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *safeLogBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *safeLogBuffer) requestLogs(t *testing.T) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid log line %q: %v", line, err)
		}
		if entry["msg"] == "request" {
			entries = append(entries, entry)
		}
	}
	return entries
}

type integrationFixture struct {
	t         *testing.T
	adminPool *pgxpool.Pool
	pool      *pgxpool.Pool
	schema    string
	origin    string
	server    *httptest.Server
	client    *http.Client
	auth      *Auth
	backend   *Backend
	logs      *safeLogBuffer

	adminID     uuid.UUID
	memberID    uuid.UUID
	adminToken  string
	memberToken string
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping backend HTTP integration test")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &integrationFixture{t: t, adminPool: adminPool, logs: &safeLogBuffer{}}
	t.Cleanup(adminPool.Close)
	f.schema = pgx.Identifier{"backend_http_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+f.schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := adminPool.Exec(cleanup, "DROP SCHEMA "+f.schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", f.schema, err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = f.schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.pool = pool
	for _, name := range integrationSchemaFiles {
		script, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "schema", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	f.adminID = f.insertPerson("Ada", "Admin", "admin@example.test", "sub-admin", "admin")
	f.memberID = f.insertPerson("Grace", "Member", "member@example.test", "sub-member", "member")
	f.adminToken = f.createSession(f.adminID)
	f.memberToken = f.createSession(f.memberID)

	logger := slog.New(slog.NewJSONHandler(f.logs, nil))
	f.server = httptest.NewUnstartedServer(nil)
	t.Cleanup(f.server.Close)
	_, port, err := net.SplitHostPort(f.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	f.origin = "http://localhost:" + port
	authService := application.NewAuth(postgres.NewAuthRepository(pool), time.Hour, nil)
	f.auth = NewAuth(integrationProvider{}, authService, AuthConfig{PublicURL: f.origin, SessionTTL: time.Hour}, logger)
	f.backend = NewBackend(application.NewBackend(postgres.NewBackendRepository(pool)), f.auth, logger)
	f.server.Config.Handler = NewRouterWithBackend(
		application.NewPeople(postgres.NewPeopleRepository(pool)),
		pool.Ping, logger, f.auth, f.backend)
	f.server.Start()
	f.client = f.server.Client()
	f.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return f
}

func (f *integrationFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), query, args...); err != nil {
		f.t.Fatalf("exec %q: %v", query, err)
	}
}

func (f *integrationFixture) withTx(fn func(tx pgx.Tx)) {
	f.t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	fn(tx)
	if err := tx.Commit(ctx); err != nil {
		f.t.Fatalf("commit fixture transaction: %v", err)
	}
}

func (f *integrationFixture) count(query string, args ...any) int {
	f.t.Helper()
	var total int
	if err := f.pool.QueryRow(context.Background(), query, args...).Scan(&total); err != nil {
		f.t.Fatalf("count %q: %v", query, err)
	}
	return total
}

func (f *integrationFixture) insertPerson(firstName, lastName, email, subject, role string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name, email, google_sub, role) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		firstName, lastName, email, subject, role).Scan(&id); err != nil {
		f.t.Fatalf("insert person %s: %v", email, err)
	}
	return id
}

func (f *integrationFixture) insertUnregistered(firstName, lastName string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name) VALUES ($1, $2) RETURNING id",
		firstName, lastName).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func integrationToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func (f *integrationFixture) createSession(personID uuid.UUID) string {
	f.t.Helper()
	token := integrationToken()
	hash := sha256.Sum256([]byte(token))
	if _, err := f.pool.Exec(context.Background(),
		"INSERT INTO sessions (token_hash, person_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')",
		hash[:], personID); err != nil {
		f.t.Fatalf("insert session: %v", err)
	}
	return token
}

func (f *integrationFixture) createExpiredSession(personID uuid.UUID) string {
	f.t.Helper()
	token := integrationToken()
	hash := sha256.Sum256([]byte(token))
	if _, err := f.pool.Exec(context.Background(),
		"INSERT INTO sessions (token_hash, person_id, created_at, expires_at) "+
			"VALUES ($1, $2, now() - interval '2 hours', now() - interval '1 hour')",
		hash[:], personID); err != nil {
		f.t.Fatalf("insert expired session: %v", err)
	}
	return token
}

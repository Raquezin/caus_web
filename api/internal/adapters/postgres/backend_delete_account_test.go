package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"caus-platform/api/internal/domain"
)

func endpointDeleteAccountPerson(t *testing.T, tx pgx.Tx, first, last, email, subject, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := tx.QueryRow(context.Background(),
		"INSERT INTO people (first_name, last_name, email, google_sub, role) VALUES ($1, $2, $3, $4, $5) RETURNING id",
		first, last, email, subject, role).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func endpointDeleteAccountSession(t *testing.T, tx pgx.Tx, personID uuid.UUID, label string) []byte {
	t.Helper()
	hash := sha256.Sum256([]byte(label))
	if _, err := tx.Exec(context.Background(),
		"INSERT INTO sessions (token_hash, person_id, expires_at) VALUES ($1, $2, now() + interval '2 hours')",
		hash[:], personID); err != nil {
		t.Fatal(err)
	}
	return hash[:]
}

func endpointDeleteAccountHistory(t *testing.T, tx pgx.Tx, personID uuid.UUID) (talkTitle, teamName, competitionName string) {
	t.Helper()
	ctx := context.Background()
	talkTitle = "Talk " + uuid.NewString()
	var talkID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO talks (title, description, date, duration, location) VALUES ($1, 'Description', '2026-03-14 09:30:00', interval '1 hour', 'Room 1') RETURNING id",
		talkTitle).Scan(&talkID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO talk_speakers (talk_id, person_id) VALUES ($1, $2)", talkID, personID); err != nil {
		t.Fatal(err)
	}
	competitionName = "Competition " + uuid.NewString()
	var competitionID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO competitions (name, description, date, location) VALUES ($1, 'Description', now(), 'Hall') RETURNING id",
		competitionName).Scan(&competitionID); err != nil {
		t.Fatal(err)
	}
	teamName = "Team " + uuid.NewString()
	var teamID uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO teams (competition_id, name) VALUES ($1, $2) RETURNING id",
		competitionID, teamName).Scan(&teamID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO team_members (team_id, competition_id, person_id) VALUES ($1, $2, $3)",
		teamID, competitionID, personID); err != nil {
		t.Fatal(err)
	}
	return talkTitle, teamName, competitionName
}

func endpointDeleteAccountHistoryRows(t *testing.T, tx pgx.Tx, personID uuid.UUID) string {
	t.Helper()
	var rows string
	if err := tx.QueryRow(context.Background(), `
		SELECT concat_ws('|',
			coalesce((SELECT string_agg(format('talk %s %s %s %s', t.id, t.title, t.date, t.duration), ',' ORDER BY t.id)
				FROM talk_speakers sp JOIN talks t ON t.id = sp.talk_id WHERE sp.person_id = $1), ''),
			coalesce((SELECT string_agg(format('team %s %s %s %s %s', tm.team_id, tm.competition_id, t.name, t.position, t.photo_url), ',' ORDER BY tm.team_id)
				FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.person_id = $1), ''),
			coalesce((SELECT string_agg(format('competition %s %s %s %s %s', c.id, c.name, c.date, c.location, c.team_size), ',' ORDER BY c.id)
				FROM team_members tm JOIN competitions c ON c.id = tm.competition_id WHERE tm.person_id = $1), '')
		)`, pgUUID(personID)).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func endpointDeleteAccountCount(t *testing.T, tx pgx.Tx, query string, args ...any) int {
	t.Helper()
	var count int
	if err := tx.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestEndpointDeleteAccountIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()

	own := endpointDeleteAccountPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	foreign := endpointDeleteAccountPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "admin")
	var ownCreated time.Time
	if err := tx.QueryRow(ctx, "SELECT created_at FROM people WHERE id = $1", pgUUID(own)).Scan(&ownCreated); err != nil {
		t.Fatal(err)
	}
	ownSessions := [][]byte{
		endpointDeleteAccountSession(t, tx, own, "own-session-one"),
		endpointDeleteAccountSession(t, tx, own, "own-session-two"),
	}
	foreignSession := endpointDeleteAccountSession(t, tx, foreign, "foreign-session")
	if _, err := tx.Exec(ctx,
		"INSERT INTO admin_email_bindings (email_hash) VALUES (sha256(convert_to('ada@example.com', 'UTF8'))), (sha256(convert_to('ada.alt@example.com', 'UTF8'))), (sha256(convert_to('grace@example.com', 'UTF8')))"); err != nil {
		t.Fatal(err)
	}
	talkTitle, teamName, competitionName := endpointDeleteAccountHistory(t, tx, own)
	before := endpointDeleteAccountHistoryRows(t, tx, own)
	for _, name := range []string{talkTitle, teamName, competitionName} {
		if !strings.Contains(before, name) {
			t.Fatalf("history snapshot %q missing %q", before, name)
		}
	}

	auth := NewAuthRepository(tx)
	for _, hash := range ownSessions {
		account, err := auth.FindSession(ctx, hash, time.Now())
		if err != nil {
			t.Fatalf("own session before delete: %v", err)
		}
		if account.ID != own {
			t.Fatalf("own session resolved account %v, want %v", account.ID, own)
		}
	}
	foreignAccount, err := auth.FindSession(ctx, foreignSession, time.Now())
	if err != nil {
		t.Fatalf("foreign session before delete: %v", err)
	}
	if foreignAccount.ID != foreign {
		t.Fatalf("foreign session resolved account %v, want %v", foreignAccount.ID, foreign)
	}

	if err := repo.DeleteAccount(ctx, own); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	var firstName, lastName, role string
	var email, subject pgtype.Text
	var created time.Time
	if err := tx.QueryRow(ctx,
		"SELECT first_name, last_name, email, google_sub, role, created_at FROM people WHERE id = $1",
		pgUUID(own)).Scan(&firstName, &lastName, &email, &subject, &role, &created); err != nil {
		t.Fatal(err)
	}
	if firstName != "Ada" || lastName != "Lovelace" || role != "member" || email.Valid || subject.Valid {
		t.Fatalf("own account after delete: %s %s %v %v %s", firstName, lastName, email, subject, role)
	}
	if !created.Equal(ownCreated) {
		t.Fatalf("history timestamp changed: %v, want %v", created, ownCreated)
	}

	var foreignFirst, foreignLast, foreignEmail, foreignSubject, foreignRole string
	if err := tx.QueryRow(ctx,
		"SELECT first_name, last_name, email, google_sub, role FROM people WHERE id = $1",
		pgUUID(foreign)).Scan(&foreignFirst, &foreignLast, &foreignEmail, &foreignSubject, &foreignRole); err != nil {
		t.Fatal(err)
	}
	if foreignFirst != "Grace" || foreignLast != "Hopper" || foreignEmail != "grace@example.com" ||
		foreignSubject != "sub-grace" || foreignRole != "admin" {
		t.Fatalf("foreign account changed: %s %s %s %s %s", foreignFirst, foreignLast, foreignEmail, foreignSubject, foreignRole)
	}

	for _, hash := range ownSessions {
		if count := endpointDeleteAccountCount(t, tx,
			"SELECT count(*) FROM sessions WHERE token_hash = $1", hash); count != 0 {
			t.Fatalf("own session survived: %d", count)
		}
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", foreignSession); count != 1 {
		t.Fatalf("foreign session removed: %d", count)
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to('ada@example.com', 'UTF8')) OR email_hash = sha256(convert_to('ada.alt@example.com', 'UTF8'))"); count != 2 {
		t.Fatalf("consumed markers removed by deletion: %d", count)
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM admin_email_bindings WHERE email_hash = sha256(convert_to('grace@example.com', 'UTF8'))"); count != 1 {
		t.Fatalf("foreign marker removed: %d", count)
	}
	for _, hash := range ownSessions {
		if _, err := auth.FindSession(ctx, hash, time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("own session after delete error %v, want %v", err, domain.ErrUnauthenticated)
		}
	}
	foreignAfter, err := auth.FindSession(ctx, foreignSession, time.Now())
	if err != nil {
		t.Fatalf("foreign session after delete: %v", err)
	}
	if foreignAfter.ID != foreign {
		t.Fatalf("foreign session resolved account %v, want %v", foreignAfter.ID, foreign)
	}

	if after := endpointDeleteAccountHistoryRows(t, tx, own); after != before {
		t.Fatalf("history changed after delete:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestEndpointDeleteAccountMissingPerson(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	person := endpointDeleteAccountPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	session := endpointDeleteAccountSession(t, tx, person, "own-session")

	err := repo.DeleteAccount(ctx, uuid.New())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing person error %v, want %v", err, domain.ErrNotFound)
	}
	var email, subject string
	if err := tx.QueryRow(ctx,
		"SELECT email, google_sub FROM people WHERE id = $1", pgUUID(person)).Scan(&email, &subject); err != nil {
		t.Fatal(err)
	}
	if email != "ada@example.com" || subject != "sub-ada" {
		t.Fatalf("missing person changed another account: %s %s", email, subject)
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", session); count != 1 {
		t.Fatalf("missing person removed a session: %d", count)
	}
}

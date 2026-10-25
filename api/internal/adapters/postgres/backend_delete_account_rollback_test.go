package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func TestEndpointDeleteAccountRollsBackWhenCommitFails(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	auth := NewAuthRepository(tx)

	own := endpointDeleteAccountPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "admin")
	session := endpointDeleteAccountSession(t, tx, own, "own-session")
	if _, err := tx.Exec(ctx,
		"INSERT INTO admin_email_bindings (email_hash) VALUES (sha256(convert_to('ada@example.com', 'UTF8'))), (sha256(convert_to('ada.alt@example.com', 'UTF8')))"); err != nil {
		t.Fatal(err)
	}
	insertTalkRow(t, tx, false)

	err := repo.DeleteAccount(ctx, own)
	var validation domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("delete account with pending constraint error %v, want validation error", err)
	}

	var email, subject, role string
	if err := tx.QueryRow(ctx,
		"SELECT email, google_sub, role FROM people WHERE id = $1", pgUUID(own)).Scan(&email, &subject, &role); err != nil {
		t.Fatal(err)
	}
	if email != "ada@example.com" || subject != "sub-ada" || role != "admin" {
		t.Fatalf("person changed after rollback: %s %s %s", email, subject, role)
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", session); count != 1 {
		t.Fatalf("session removed after rollback: %d", count)
	}
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM admin_email_bindings"); count != 2 {
		t.Fatalf("markers changed after rollback: %d", count)
	}

	account, err := auth.FindSession(ctx, session, time.Now())
	if err != nil {
		t.Fatalf("session after rollback: %v", err)
	}
	if account.ID != own {
		t.Fatalf("session after rollback resolved account %v, want %v", account.ID, own)
	}
	endpointDeleteAccountSession(t, tx, own, "post-rollback-session")
	if count := endpointDeleteAccountCount(t, tx,
		"SELECT count(*) FROM sessions WHERE person_id = $1", pgUUID(own)); count != 2 {
		t.Fatalf("post-rollback write not visible: %d sessions", count)
	}
}

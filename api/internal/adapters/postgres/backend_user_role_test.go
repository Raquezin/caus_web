package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

func TestEndpointUserRoleIntegration(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	personID := insertPersonRow(t, tx, "Ada", "Lovelace")
	insertExportPerson(t, tx, "Alan", "Turing", "alan@example.com", "sub-alan", "admin")
	if _, err := tx.Exec(ctx,
		"UPDATE people SET email = $2, google_sub = $3 WHERE id = $1",
		personID, "ada@example.com", "google-subject"); err != nil {
		t.Fatal(err)
	}
	tokenHash := sha256.Sum256([]byte("existing-session-token"))
	if _, err := tx.Exec(ctx,
		"INSERT INTO sessions (token_hash, person_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')",
		tokenHash[:], personID); err != nil {
		t.Fatal(err)
	}
	auth := NewAuthRepository(tx)
	before, err := auth.FindSession(ctx, tokenHash[:], time.Now())
	if err != nil || before.Role != "member" {
		t.Fatalf("initial session %+v (%v)", before, err)
	}

	user, err := repo.UpdateUserRole(ctx, personID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != personID || user.Role != "admin" || user.Email == nil || *user.Email != "ada@example.com" {
		t.Fatalf("promoted user %+v", user)
	}
	live, err := auth.FindSession(ctx, tokenHash[:], time.Now())
	if err != nil || live.Role != "admin" || live.ID != personID {
		t.Fatalf("existing session after promotion %+v (%v)", live, err)
	}

	demoted, err := repo.UpdateUserRole(ctx, personID, "member")
	if err != nil {
		t.Fatal(err)
	}
	if demoted.ID != personID || demoted.Role != "member" {
		t.Fatalf("demoted user %+v", demoted)
	}
	live, err = auth.FindSession(ctx, tokenHash[:], time.Now())
	if err != nil || live.Role != "member" {
		t.Fatalf("existing session after demotion %+v (%v)", live, err)
	}

	if _, err := repo.UpdateUserRole(ctx, uuid.New(), "admin"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing user error %v", err)
	}
}

func TestEndpointUserRoleLeavesOtherPeopleUntouched(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	targetID := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "member")
	otherID := insertPersonRow(t, tx, "Grace", "Hopper")
	if _, err := repo.UpdateUserRole(ctx, targetID, "admin"); err != nil {
		t.Fatal(err)
	}
	other, err := readUser(ctx, tx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if other.Role != "member" {
		t.Fatalf("unrelated role changed to %q", other.Role)
	}
	user, err := readUser(ctx, tx, targetID)
	if err != nil || user.Role != "admin" {
		t.Fatalf("updated role %+v (%v)", user, err)
	}
}

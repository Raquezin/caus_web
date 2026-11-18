package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"caus-platform/api/internal/domain"
)

func backendAccountUpdateNames(t *testing.T, tx pgx.Tx, id uuid.UUID) (string, string) {
	t.Helper()
	var first, last string
	if err := tx.QueryRow(context.Background(),
		"SELECT first_name, last_name FROM people WHERE id = $1", pgUUID(id)).Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	return first, last
}

func TestBackendAccountUpdateChangesOnlyProvidedNames(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	id := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "member")
	otherID := insertExportPerson(t, tx, "Grace", "Hopper", "grace@example.com", "sub-grace", "admin")

	firstName := "Ada Maria"
	account, err := repo.UpdateAccount(ctx, id, domain.AccountPatch{FirstName: &firstName})
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != id || account.FirstName != firstName || account.LastName != "Lovelace" ||
		account.Email != "ada@example.com" || account.Role != "member" {
		t.Fatalf("account after first name update %+v", account)
	}
	if first, last := backendAccountUpdateNames(t, tx, id); first != firstName || last != "Lovelace" {
		t.Fatalf("stored names %q %q", first, last)
	}

	lastName := "Byron"
	account, err = repo.UpdateAccount(ctx, id, domain.AccountPatch{LastName: &lastName})
	if err != nil {
		t.Fatal(err)
	}
	if account.FirstName != firstName || account.LastName != lastName {
		t.Fatalf("account after last name update %+v", account)
	}
	if first, last := backendAccountUpdateNames(t, tx, id); first != firstName || last != lastName {
		t.Fatalf("stored names %q %q", first, last)
	}

	if first, last := backendAccountUpdateNames(t, tx, otherID); first != "Grace" || last != "Hopper" {
		t.Fatalf("other person changed to %q %q", first, last)
	}
}

func TestBackendAccountUpdateStoresEmptyLastName(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	id := insertExportPerson(t, tx, "Ada", "Lovelace", "ada@example.com", "sub-ada", "member")

	empty := ""
	account, err := repo.UpdateAccount(ctx, id, domain.AccountPatch{LastName: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if account.LastName != "" || account.FirstName != "Ada" {
		t.Fatalf("account with empty last name %+v", account)
	}
	if first, last := backendAccountUpdateNames(t, tx, id); first != "Ada" || last != "" {
		t.Fatalf("stored names %q %q", first, last)
	}
}

func TestBackendAccountUpdateAdvancesUpdatedAt(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	var id uuid.UUID
	if err := tx.QueryRow(ctx,
		"INSERT INTO people (first_name, last_name, email, google_sub, updated_at) "+
			"VALUES ('Ada', 'Lovelace', 'ada@example.com', 'sub-ada', now() - interval '1 day') RETURNING id").
		Scan(&id); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := tx.QueryRow(ctx, "SELECT updated_at FROM people WHERE id = $1", pgUUID(id)).Scan(&before); err != nil {
		t.Fatal(err)
	}

	firstName := "Augusta"
	account, err := repo.UpdateAccount(ctx, id, domain.AccountPatch{FirstName: &firstName})
	if err != nil {
		t.Fatal(err)
	}
	if !account.UpdatedAt.After(before) {
		t.Fatalf("updated_at %v did not advance past %v", account.UpdatedAt, before)
	}
}

func TestBackendAccountUpdateRejectsDeletedAndUnknownAccounts(t *testing.T) {
	repo, tx := backendFixture(t)
	ctx := context.Background()
	deleted := insertPersonRow(t, tx, "Grace", "Hopper")
	firstName := "Grace"
	for _, id := range []uuid.UUID{deleted, uuid.New()} {
		_, err := repo.UpdateAccount(ctx, id, domain.AccountPatch{FirstName: &firstName})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("account %s error %v, want %v", id, err, domain.ErrNotFound)
		}
	}
	if first, last := backendAccountUpdateNames(t, tx, deleted); first != "Grace" || last != "Hopper" {
		t.Fatalf("deleted account changed to %q %q", first, last)
	}
}

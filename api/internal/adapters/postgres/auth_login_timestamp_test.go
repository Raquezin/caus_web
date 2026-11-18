package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func TestCompleteLoginLeavesUpdatedAtAloneWhenNothingChanges(t *testing.T) {
	_, tx := backendFixture(t)
	pool := endpointExportDatabase(t, tx)
	repo := NewAuthRepository(pool)
	ctx := context.Background()
	expires := time.Now().Add(time.Hour)
	logins := 0
	login := func(identity domain.GoogleIdentity) domain.Account {
		t.Helper()
		logins++
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", identity.Subject, logins)))
		account, err := repo.CompleteLogin(ctx, identity, hash[:], expires, false)
		if err != nil {
			t.Fatal(err)
		}
		return account
	}
	identity := domain.GoogleIdentity{Subject: "stable-sub", Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"}
	first := login(identity)
	time.Sleep(2 * time.Millisecond)
	second := login(identity)
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("unchanged login moved updated_at from %v to %v", first.UpdatedAt, second.UpdatedAt)
	}
	changed := identity
	changed.Email = "ada-new@example.com"
	time.Sleep(2 * time.Millisecond)
	third := login(changed)
	if !third.UpdatedAt.After(second.UpdatedAt) {
		t.Fatalf("changed login kept updated_at %v", third.UpdatedAt)
	}
}

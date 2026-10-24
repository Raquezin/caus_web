package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"caus-platform/api/internal/domain"
)

type authRepositoryStub struct {
	identity  domain.GoogleIdentity
	hash      []byte
	expires   time.Time
	now       time.Time
	promote   bool
	account   domain.Account
	err       error
	deleteErr error
	calls     int
	deleted   bool
	ctx       context.Context
}

func (r *authRepositoryStub) CompleteLogin(ctx context.Context, identity domain.GoogleIdentity, hash []byte, expires time.Time, promote bool) (domain.Account, error) {
	r.calls++
	r.ctx, r.identity, r.hash, r.expires, r.promote = ctx, identity, bytes.Clone(hash), expires, promote
	return r.account, r.err
}

func (r *authRepositoryStub) FindSession(ctx context.Context, hash []byte, now time.Time) (domain.Account, error) {
	r.calls++
	r.ctx, r.hash, r.now = ctx, bytes.Clone(hash), now
	if r.err != nil {
		return domain.Account{}, r.err
	}
	if !now.Before(r.expires) || r.deleted {
		return domain.Account{}, domain.ErrUnauthenticated
	}
	return r.account, nil
}

func (r *authRepositoryStub) DeleteSession(ctx context.Context, hash []byte) error {
	r.calls++
	r.ctx, r.hash, r.deleted = ctx, bytes.Clone(hash), true
	return r.deleteErr
}

func testIdentity() domain.GoogleIdentity {
	return domain.GoogleIdentity{Subject: " opaque subject ", Email: " Ada@Example.COM ", FirstName: "Ada", LastName: "Lovelace"}
}

func TestAuthLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &authRepositoryStub{account: domain.Account{ID: uuid.New()}}
	auth := NewAuth(repo, 720*time.Hour, []string{" ADA@example.com "})
	auth.now = func() time.Time { return now }
	account, raw, err := auth.Login(ctx, testIdentity())
	if err != nil || account != repo.account {
		t.Fatalf("login result: %v", err)
	}
	random, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	wantHash := sha256.Sum256([]byte(raw))
	if err != nil || len(raw) != 43 || len(random) != 32 || !bytes.Equal(repo.hash, wantHash[:]) || bytes.Equal(repo.hash, random) {
		t.Fatal("session must hash the canonical raw string, not decoded random bytes")
	}
	if repo.identity.Email != "ada@example.com" || repo.identity.Subject != testIdentity().Subject ||
		repo.identity.FirstName != "Ada" || repo.identity.LastName != "Lovelace" || !repo.promote ||
		!repo.expires.Equal(now.Add(720*time.Hour)) || repo.ctx != ctx {
		t.Fatal("normalization, opaque subject, profile, promotion, expiry or context changed")
	}
	_, second, err := auth.Login(ctx, testIdentity())
	if err != nil || second == raw {
		t.Fatal("sessions must be independently random")
	}
}

func TestAuthRejectsIncompleteIdentity(t *testing.T) {
	cases := map[string]func(*domain.GoogleIdentity){
		"subject":        func(i *domain.GoogleIdentity) { i.Subject = " \t" },
		"first name":     func(i *domain.GoogleIdentity) { i.FirstName = "\u2003" },
		"last name":      func(i *domain.GoogleIdentity) { i.LastName = " " },
		"email":          func(i *domain.GoogleIdentity) { i.Email = "" },
		"display email":  func(i *domain.GoogleIdentity) { i.Email = "Ada <ada@example.com>" },
		"domain dot":     func(i *domain.GoogleIdentity) { i.Email = "ada@localhost" },
		"space":          func(i *domain.GoogleIdentity) { i.Email = "ada@exam ple.com" },
		"unicode space":  func(i *domain.GoogleIdentity) { i.Email = "ada@exam\u2003ple.com" },
		"multiple email": func(i *domain.GoogleIdentity) { i.Email = "ada@example.com,x@example.com" },
		"bad local":      func(i *domain.GoogleIdentity) { i.Email = "a..b@example.com" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			identity := testIdentity()
			mutate(&identity)
			repo := &authRepositoryStub{}
			auth := NewAuth(repo, time.Hour, nil)
			account, raw, err := auth.Login(context.Background(), identity)
			if !errors.Is(err, domain.ErrInvalidIdentity) || raw != "" || account != (domain.Account{}) || repo.calls != 0 {
				t.Fatal("invalid identity reached persistence or exposed a session")
			}
		})
	}
}

func TestAuthRejectsInvalidTTL(t *testing.T) {
	for _, ttl := range []time.Duration{-time.Second, 0, time.Nanosecond, 8760*time.Hour + time.Nanosecond} {
		repo := &authRepositoryStub{}
		auth := NewAuth(repo, ttl, nil)
		if _, raw, err := auth.Login(context.Background(), testIdentity()); err == nil || raw != "" || repo.calls != 0 {
			t.Fatal("invalid session TTL reached persistence")
		}
	}
}

func TestAuthSessionValidationAndExpiry(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	hash := sha256.Sum256([]byte(raw))
	repo := &authRepositoryStub{account: domain.Account{ID: uuid.New()}, expires: now.Add(time.Hour)}
	auth := NewAuth(repo, time.Hour, nil)
	auth.now = func() time.Time { return now }
	ctx := context.Background()
	for _, token := range []string{"", raw + "=", raw[:42], strings.Repeat("!", 43), raw[:42] + "B", " " + raw, raw + "\n"} {
		if _, err := auth.CurrentUser(ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("invalid session format accepted")
		}
		if err := auth.Logout(ctx, token); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal("invalid session format accepted for logout")
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid token reached repository")
	}
	account, err := auth.CurrentUser(ctx, raw)
	if err != nil || account != repo.account || !repo.now.Equal(now) || !bytes.Equal(repo.hash, hash[:]) {
		t.Fatal("current session not found by raw-string hash and clock")
	}
	now = repo.expires
	if _, err := auth.CurrentUser(ctx, raw); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("exact expiry boundary accepted")
	}
	if err := auth.Logout(ctx, raw); !errors.Is(err, domain.ErrUnauthenticated) || repo.deleted {
		t.Fatal("expired session authenticated for logout")
	}
	now = now.Add(-time.Second)
	if err := auth.Logout(ctx, raw); err != nil || !repo.deleted || !bytes.Equal(repo.hash, hash[:]) {
		t.Fatal("logout failed to delete authenticated session hash")
	}
	if _, err := auth.CurrentUser(ctx, raw); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted session accepted")
	}
}

func TestAuthErrorsDoNotReturnPartialCredentials(t *testing.T) {
	for _, failure := range []error{domain.ErrIdentityConflict, context.Canceled, errors.New("private database detail")} {
		repo := &authRepositoryStub{err: failure, account: domain.Account{ID: uuid.New()}}
		auth := NewAuth(repo, time.Hour, nil)
		account, raw, err := auth.Login(context.Background(), testIdentity())
		if !errors.Is(err, failure) || raw != "" || account != (domain.Account{}) {
			t.Fatal("login lost wrapped error or exposed partial credentials")
		}
		token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
		if account, err := auth.CurrentUser(context.Background(), token); !errors.Is(err, failure) || account != (domain.Account{}) {
			t.Fatal("current user lost wrapped error or exposed partial account")
		}
		if err := auth.Logout(context.Background(), token); !errors.Is(err, failure) || repo.deleted {
			t.Fatal("logout lost wrapped error or deleted without authentication")
		}
	}
	failure := errors.New("private delete error")
	repo := &authRepositoryStub{expires: time.Now().Add(time.Hour), deleteErr: failure}
	auth := NewAuth(repo, time.Hour, nil)
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if err := auth.Logout(context.Background(), token); !errors.Is(err, failure) || !repo.deleted {
		t.Fatal("authenticated deletion failure lost its wrapped error")
	}
}

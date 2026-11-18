package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"caus-platform/api/internal/domain"
)

func TestAuthRejectsInvalidPersistedText(t *testing.T) {
	cases := map[string]func(*domain.GoogleIdentity){
		"subject NUL":        func(i *domain.GoogleIdentity) { i.Subject = "a\x00b" },
		"subject control":    func(i *domain.GoogleIdentity) { i.Subject = "a\x01b" },
		"subject del":        func(i *domain.GoogleIdentity) { i.Subject = "a\x7fb" },
		"subject utf8":       func(i *domain.GoogleIdentity) { i.Subject = "a\xffb" },
		"first name NUL":     func(i *domain.GoogleIdentity) { i.FirstName = "a\x00b" },
		"first name control": func(i *domain.GoogleIdentity) { i.FirstName = "a\x1fb" },
		"first name del":     func(i *domain.GoogleIdentity) { i.FirstName = "a\x7fb" },
		"first name utf8":    func(i *domain.GoogleIdentity) { i.FirstName = "a\xffb" },
		"first name bom":     func(i *domain.GoogleIdentity) { i.FirstName = "\uFEFF" },
		"last name NUL":      func(i *domain.GoogleIdentity) { i.LastName = "a\x00b" },
		"last name control":  func(i *domain.GoogleIdentity) { i.LastName = "a\x02b" },
		"last name del":      func(i *domain.GoogleIdentity) { i.LastName = "a\x7fb" },
		"last name utf8":     func(i *domain.GoogleIdentity) { i.LastName = "a\xffb" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			identity := testIdentity()
			mutate(&identity)
			repo := &authRepositoryStub{}
			auth := NewAuth(repo, time.Hour, nil)
			account, raw, err := auth.Login(context.Background(), identity)
			if !errors.Is(err, domain.ErrInvalidIdentity) || raw != "" || account != (domain.Account{}) || repo.calls != 0 {
				t.Fatal("invalid persisted text reached persistence or exposed a session")
			}
		})
	}
}

func TestAuthAllowsMissingLastName(t *testing.T) {
	for name, lastName := range map[string]string{"blank": "", "spaces": " \t"} {
		t.Run(name, func(t *testing.T) {
			repo := &authRepositoryStub{}
			identity := testIdentity()
			identity.LastName = lastName
			if _, raw, err := NewAuth(repo, time.Hour, nil).Login(context.Background(), identity); err != nil || raw == "" {
				t.Fatalf("login with last name %q: %v", lastName, err)
			}
			if repo.identity.LastName != "" {
				t.Fatalf("last name reached repository as %q", repo.identity.LastName)
			}
		})
	}
}

func TestAuthTrimsNames(t *testing.T) {
	repo := &authRepositoryStub{}
	identity := testIdentity()
	identity.FirstName, identity.LastName = "  Ada  ", "  "
	if _, _, err := NewAuth(repo, time.Hour, nil).Login(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if repo.identity.FirstName != "Ada" || repo.identity.LastName != "" {
		t.Fatalf("names reached repository as %q %q", repo.identity.FirstName, repo.identity.LastName)
	}
	identity = testIdentity()
	identity.FirstName = " \t "
	if _, raw, err := NewAuth(&authRepositoryStub{}, time.Hour, nil).Login(context.Background(), identity); !errors.Is(err, domain.ErrInvalidIdentity) || raw != "" {
		t.Fatal("blank first name accepted")
	}
}

func TestAuthAllowsMultibyteAndWhitespacePersistedText(t *testing.T) {
	repo := &authRepositoryStub{}
	auth := NewAuth(repo, time.Hour, nil)
	identity := testIdentity()
	identity.FirstName = "María José\t"
	identity.LastName = "García\nLópez"
	if _, raw, err := auth.Login(context.Background(), identity); err != nil || raw == "" {
		t.Fatalf("legitimate multibyte identity rejected: %v", err)
	}
}

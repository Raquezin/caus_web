package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"

	"caus-platform/api/internal/domain"
)

type AuthRepository interface {
	CompleteLogin(ctx context.Context, identity domain.GoogleIdentity, tokenHash []byte, expiresAt time.Time, promote bool) (domain.Account, error)
	FindSession(ctx context.Context, hash []byte, now time.Time) (domain.Account, error)
	DeleteSession(ctx context.Context, hash []byte) error
}

type Auth struct {
	repo        AuthRepository
	ttl         time.Duration
	adminEmails map[string]bool
	now         func() time.Time
}

func NewAuth(repo AuthRepository, ttl time.Duration, adminEmails []string) *Auth {
	admins := make(map[string]bool, len(adminEmails))
	for _, email := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(email))] = true
	}
	return &Auth{repo: repo, ttl: ttl, adminEmails: admins, now: time.Now}
}

var canonicalEmail = regexp.MustCompile(`^[^[:space:]@]+@[^[:space:]@]+[.][^[:space:]@]+$`)

func (a *Auth) Login(ctx context.Context, identity domain.GoogleIdentity) (domain.Account, string, error) {
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	address, err := mail.ParseAddress(identity.Email)
	if strings.TrimSpace(identity.Subject) == "" || strings.TrimSpace(identity.FirstName) == "" ||
		strings.TrimSpace(identity.LastName) == "" || err != nil || address.Address != identity.Email ||
		!canonicalEmail.MatchString(identity.Email) || strings.ContainsFunc(identity.Email, unicode.IsSpace) {
		return domain.Account{}, "", domain.ErrInvalidIdentity
	}
	if a.ttl < time.Second || a.ttl > 8760*time.Hour {
		return domain.Account{}, "", fmt.Errorf("login: invalid session duration")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return domain.Account{}, "", fmt.Errorf("generate session: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	hash := sha256.Sum256([]byte(token))
	account, err := a.repo.CompleteLogin(ctx, identity, hash[:], a.now().Add(a.ttl), a.adminEmails[identity.Email])
	if err != nil {
		return domain.Account{}, "", fmt.Errorf("login: %w", err)
	}
	return account, token, nil
}

func sessionHash(token string) ([]byte, error) {
	if len(token) != 43 {
		return nil, domain.ErrUnauthenticated
	}
	random, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(random) != 32 || base64.RawURLEncoding.EncodeToString(random) != token {
		return nil, domain.ErrUnauthenticated
	}
	hash := sha256.Sum256([]byte(token))
	return hash[:], nil
}

func (a *Auth) CurrentUser(ctx context.Context, token string) (domain.Account, error) {
	hash, err := sessionHash(token)
	if err != nil {
		return domain.Account{}, err
	}
	account, err := a.repo.FindSession(ctx, hash, a.now())
	if err != nil {
		return domain.Account{}, fmt.Errorf("current user: %w", err)
	}
	return account, nil
}

func (a *Auth) Logout(ctx context.Context, token string) error {
	hash, err := sessionHash(token)
	if err != nil {
		return err
	}
	if _, err := a.repo.FindSession(ctx, hash, a.now()); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	if err := a.repo.DeleteSession(ctx, hash); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

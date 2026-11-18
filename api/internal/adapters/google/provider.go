package google

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

const requestTimeout = 10 * time.Second

type Provider struct {
	initialization chan struct{}
	issuer         string
	config         oauth2.Config
	verifier       *oidc.IDTokenVerifier
	client         *http.Client
}

func New(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) *Provider {
	client := http.DefaultClient
	if configured, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && configured != nil {
		client = configured
	}
	boundedClient := *client
	boundedClient.Timeout = requestTimeout
	return &Provider{
		initialization: make(chan struct{}, 1),
		issuer:         issuer,
		config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		client: &boundedClient,
	}
}

func (p *Provider) initialize(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	select {
	case p.initialization <- struct{}{}:
		defer func() { <-p.initialization }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.verifier != nil {
		return nil
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, p.client), p.issuer)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrAuthUnavailable, fmt.Errorf("discover oidc provider: %w", err))
	}
	p.config.Endpoint = provider.Endpoint()
	p.verifier = provider.Verifier(&oidc.Config{ClientID: p.config.ClientID})
	return nil
}

func (p *Provider) AuthorizationURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	if err := p.initialize(ctx); err != nil {
		return "", err
	}
	return p.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (domain.GoogleIdentity, error) {
	if code == "" || verifier == "" || nonce == "" {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	if err := p.initialize(ctx); err != nil {
		if ctx.Err() != nil {
			return domain.GoogleIdentity{}, ctx.Err()
		}
		return domain.GoogleIdentity{}, err
	}
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		var retrieveError *oauth2.RetrieveError
		if errors.As(err, &retrieveError) && retrieveError.ErrorCode == "invalid_grant" {
			return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
		}
		return domain.GoogleIdentity{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		if ctx.Err() != nil {
			return domain.GoogleIdentity{}, ctx.Err()
		}
		if strings.HasPrefix(err.Error(), "failed to verify signature: fetching keys ") {
			return domain.GoogleIdentity{}, fmt.Errorf("%w: %w", domain.ErrAuthUnavailable, err)
		}
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	if idToken.Nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	var claims struct {
		Email           string `json:"email"`
		EmailVerified   bool   `json:"email_verified"`
		FirstName       string `json:"given_name"`
		LastName        string `json:"family_name"`
		AuthorizedParty string `json:"azp"`
		HostedDomain    string `json:"hd"`
	}
	if err := idToken.Claims(&claims); err != nil || !claims.EmailVerified || claims.Email == "" || idToken.Subject == "" {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	if claims.AuthorizedParty != p.config.ClientID && (claims.AuthorizedParty != "" || len(idToken.Audience) > 1) {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	return domain.GoogleIdentity{
		Subject:   idToken.Subject,
		Email:     claims.Email,
		FirstName: claims.FirstName,
		LastName:  claims.LastName,
		EmailAuthoritative: strings.HasSuffix(strings.ToLower(strings.TrimSpace(claims.Email)), "@gmail.com") ||
			strings.TrimSpace(claims.HostedDomain) != "",
	}, nil
}

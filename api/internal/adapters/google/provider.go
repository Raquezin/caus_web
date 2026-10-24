package google

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"caus-platform/api/internal/domain"
)

const requestTimeout = 10 * time.Second

type Provider struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func New(ctx context.Context, issuer, clientID, clientSecret, redirectURL string) (*Provider, error) {
	client := http.DefaultClient
	if configured, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && configured != nil {
		client = configured
	}
	boundedClient := *client
	boundedClient.Timeout = requestTimeout
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, &boundedClient), issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider: %w", err)
	}
	return &Provider{
		config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
		client:   &boundedClient,
	}, nil
}

func (p *Provider) AuthorizationURL(state, nonce, verifier string) string {
	return p.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

func (p *Provider) Exchange(ctx context.Context, code, verifier, nonce string) (domain.GoogleIdentity, error) {
	if code == "" || verifier == "" || nonce == "" {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	ctx = oidc.ClientContext(ctx, p.client)
	token, err := p.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
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
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	if idToken.Nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		FirstName     string `json:"given_name"`
		LastName      string `json:"family_name"`
	}
	if err := idToken.Claims(&claims); err != nil || !claims.EmailVerified || claims.Email == "" || idToken.Subject == "" {
		return domain.GoogleIdentity{}, domain.ErrInvalidIdentity
	}
	return domain.GoogleIdentity{
		Subject:   idToken.Subject,
		Email:     claims.Email,
		FirstName: claims.FirstName,
		LastName:  claims.LastName,
	}, nil
}

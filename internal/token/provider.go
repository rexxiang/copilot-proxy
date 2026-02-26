package token

import (
	"context"
	"errors"

	"copilot-proxy/internal/config"
)

var (
	ErrMissingGitHubToken = errors.New("missing GitHub token")
	ErrNilContext         = errors.New("nil context")
)

// DirectProvider uses GitHub OAuth access_token directly as upstream bearer token.
type DirectProvider struct{}

// NewDirectProvider creates a token provider that directly returns account.GhToken.
func NewDirectProvider() *DirectProvider {
	return &DirectProvider{}
}

// GetToken returns the GitHub OAuth token from account.
func (p *DirectProvider) GetToken(ctx context.Context, account config.Account) (string, error) {
	if ctx == nil {
		return "", ErrNilContext
	}
	if account.GhToken == "" {
		return "", ErrMissingGitHubToken
	}
	return account.GhToken, nil
}

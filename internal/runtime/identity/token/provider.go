package token

import (
	"context"
	"errors"

	"copilot-proxy/internal/runtime/config"
)

var (
	ErrMissingGitHubToken = errors.New("missing GitHub token")
	ErrNilContext         = errors.New("nil context")
)

// Resolve returns the GitHub OAuth token from account.
func Resolve(ctx context.Context, account config.Account) (string, error) {
	if ctx == nil {
		return "", ErrNilContext
	}
	if account.GhToken == "" {
		return "", ErrMissingGitHubToken
	}
	return account.GhToken, nil
}

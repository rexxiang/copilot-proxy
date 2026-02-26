package token

import (
	"context"
	"errors"
	"testing"

	"copilot-proxy/internal/config"
)

func TestDirectProviderReturnsGitHubToken(t *testing.T) {
	provider := NewDirectProvider()
	tokenValue, err := provider.GetToken(context.Background(), config.Account{User: "u1", GhToken: "gho_xxx"})
	if err != nil {
		t.Fatalf("GetToken returned error: %v", err)
	}
	if tokenValue != "gho_xxx" {
		t.Fatalf("unexpected token: %q", tokenValue)
	}
}

func TestDirectProviderRejectsMissingToken(t *testing.T) {
	provider := NewDirectProvider()
	_, err := provider.GetToken(context.Background(), config.Account{User: "u1"})
	if !errors.Is(err, ErrMissingGitHubToken) {
		t.Fatalf("expected ErrMissingGitHubToken, got %v", err)
	}
}

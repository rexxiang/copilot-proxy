package token

import (
	"context"
	"errors"
	"testing"

	"copilot-proxy/internal/runtime/config"
)

func TestResolveReturnsGitHubToken(t *testing.T) {
	tokenValue, err := Resolve(context.Background(), config.Account{User: "u1", GhToken: "gho_xxx"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if tokenValue != "gho_xxx" {
		t.Fatalf("unexpected token: %q", tokenValue)
	}
}

func TestResolveRejectsMissingToken(t *testing.T) {
	_, err := Resolve(context.Background(), config.Account{User: "u1"})
	if !errors.Is(err, ErrMissingGitHubToken) {
		t.Fatalf("expected ErrMissingGitHubToken, got %v", err)
	}
}

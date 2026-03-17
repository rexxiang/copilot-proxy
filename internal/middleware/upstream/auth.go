package upstream

import (
	"fmt"
	"net/http"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
)

// AuthStore combines loading and saving authentication configuration.
type AuthStore interface {
	LoadAuth() (config.AuthConfig, error)
	SaveAuth(config.AuthConfig) error
}

// ResolveAccountMiddleware loads auth and ensures a default account is selected.
type ResolveAccountMiddleware struct {
	store AuthStore
}

// NewResolveAccount builds an account resolving middleware.
func NewResolveAccount(store AuthStore) ResolveAccountMiddleware {
	return ResolveAccountMiddleware{store: store}
}

func (m ResolveAccountMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}
	if m.store == nil {
		return jsonErrorResponse(ctx.Request, http.StatusBadGateway, "failed to load auth"), nil
	}

	auth, ok := loadAuthConfig(m.store)
	if !ok {
		return jsonErrorResponse(ctx.Request, http.StatusBadGateway, "failed to load auth"), nil
	}

	account, changed, ok := resolveDefaultAccount(&auth)
	if !ok {
		return jsonErrorResponse(ctx.Request, http.StatusUnauthorized, "no account available"), nil
	}

	if changed && !persistDefaultAccount(m.store, auth) {
		return jsonErrorResponse(ctx.Request, http.StatusBadGateway, "failed to persist default account"), nil
	}

	rc := ensureRequestContext(ctx.Request)
	rc.Account = account
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

func loadAuthConfig(store AuthStore) (config.AuthConfig, bool) {
	auth, err := store.LoadAuth()
	return auth, err == nil
}

func resolveDefaultAccount(auth *config.AuthConfig) (config.Account, bool, bool) {
	if auth == nil {
		var zero config.Account
		return zero, false, false
	}
	account, changed, err := auth.DefaultAccount()
	if err != nil {
		var zero config.Account
		return zero, false, false
	}
	return account, changed, true
}

func persistDefaultAccount(store AuthStore, auth config.AuthConfig) bool {
	return saveAuthSafely(store, auth) == nil
}

func saveAuthSafely(store AuthStore, updated config.AuthConfig) error {
	current, err := store.LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth config: %w", err)
	}
	current.Default = updated.Default
	if err := store.SaveAuth(current); err != nil {
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

var _ middleware.Middleware = (*ResolveAccountMiddleware)(nil)

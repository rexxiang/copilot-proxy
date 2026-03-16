package account

import (
	"fmt"

	"copilot-proxy/internal/config"
)

// ActivateDefaultAccount sets the default account with optional persistence.
func ActivateDefaultAccount(auth *config.AuthConfig, user string, save func(config.AuthConfig) error) error {
	if auth == nil {
		return config.ErrNoAccountsConfigured
	}
	previousDefault := auth.Default
	if err := auth.SetDefault(user); err != nil {
		return err
	}
	if save == nil {
		return nil
	}
	if err := save(*auth); err != nil {
		auth.Default = previousDefault
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

// UpsertAccountPreserveDefault adds or updates an account without changing default unnecessarily.
func UpsertAccountPreserveDefault(auth *config.AuthConfig, account config.Account, save func(config.AuthConfig) error) error {
	if auth == nil {
		return config.ErrNoAccountsConfigured
	}
	previous := *auth
	hadAccounts := len(auth.Accounts) > 0
	previousDefault := auth.Default
	auth.UpsertAccount(account)
	if hadAccounts && previousDefault != "" && previousDefault != account.User {
		if err := auth.SetDefault(previousDefault); err != nil {
			*auth = previous
			return err
		}
	}
	if save == nil {
		return nil
	}
	if err := save(*auth); err != nil {
		*auth = previous
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

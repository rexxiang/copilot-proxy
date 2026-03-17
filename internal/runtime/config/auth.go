package config

import (
	"errors"
	"fmt"

	configstore "copilot-proxy/internal/runtime/config/store"
)

type Account struct {
	User    string `json:"user"`
	GhToken string `json:"gh_token"`
	AppID   string `json:"app_id"`
}

type AuthConfig struct {
	Default  string    `json:"@default"`
	Accounts []Account `json:"accounts"`
}

var (
	ErrAccountNotFound        = errors.New("account not found")
	ErrDefaultAccountNotFound = errors.New("default account not found")
	ErrInvalidConfigPath      = errors.New("invalid config path")
	ErrNoAccountsConfigured   = errors.New("no accounts configured")
)

func ConfigDir() (string, error) {
	return configstore.ConfigDir()
}

func AuthPath() (string, error) {
	return configstore.Path("auth.json")
}

func LoadAuth() (AuthConfig, error) {
	return loadJSON(AuthPath, AuthConfig{})
}

func SaveAuth(auth AuthConfig) error {
	return saveJSON(AuthPath, auth)
}

func (auth *AuthConfig) EnsureDefault() bool {
	if len(auth.Accounts) == 0 {
		return false
	}

	for _, account := range auth.Accounts {
		if account.User == auth.Default {
			return false
		}
	}

	auth.Default = auth.Accounts[0].User
	return true
}

func (auth *AuthConfig) DefaultAccount() (Account, bool, error) {
	if len(auth.Accounts) == 0 {
		return Account{}, false, ErrNoAccountsConfigured
	}

	changed := auth.EnsureDefault()
	for _, account := range auth.Accounts {
		if account.User == auth.Default {
			return account, changed, nil
		}
	}

	return Account{}, changed, ErrDefaultAccountNotFound
}

func (auth *AuthConfig) UpsertAccount(account Account) {
	for i, existing := range auth.Accounts {
		if existing.User == account.User {
			auth.Accounts[i] = account
			auth.Default = account.User
			return
		}
	}
	auth.Accounts = append(auth.Accounts, account)
	auth.Default = account.User
}

func (auth *AuthConfig) RemoveAccount(user string) bool {
	if user == "" {
		return false
	}
	for i, account := range auth.Accounts {
		if account.User == user {
			auth.Accounts = append(auth.Accounts[:i], auth.Accounts[i+1:]...)
			if auth.Default == user {
				auth.Default = ""
				auth.EnsureDefault()
			}
			return true
		}
	}
	return false
}

func (auth *AuthConfig) SetDefault(user string) error {
	for _, account := range auth.Accounts {
		if account.User == user {
			auth.Default = user
			return nil
		}
	}
	return ErrAccountNotFound
}

func loadJSON[T any](pathFunc func() (string, error), defaultVal T) (T, error) {
	path, err := pathFunc()
	if err != nil {
		return defaultVal, fmt.Errorf("resolve config path: %w", err)
	}
	result, err := configstore.LoadJSON(path, defaultVal)
	if err != nil {
		if errors.Is(err, configstore.ErrInvalidPath) {
			return defaultVal, fmt.Errorf("%w: %s", ErrInvalidConfigPath, path)
		}
		return defaultVal, err
	}
	return result, nil
}

func saveJSON[T any](pathFunc func() (string, error), value T) error {
	path, err := pathFunc()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if err := configstore.SaveJSON(path, value); err != nil {
		if errors.Is(err, configstore.ErrInvalidPath) {
			return fmt.Errorf("%w: %s", ErrInvalidConfigPath, path)
		}
		return err
	}
	return nil
}

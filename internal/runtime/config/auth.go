package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

const (
	configFileMode = 0o600
	configDirMode  = 0o700
)

var (
	ErrAccountNotFound        = errors.New("account not found")
	ErrDefaultAccountNotFound = errors.New("default account not found")
	ErrInvalidConfigPath      = errors.New("invalid config path")
	ErrNoAccountsConfigured   = errors.New("no accounts configured")
)

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".config", "copilot-proxy"), nil
}

func AuthPath() (string, error) {
	return configPath("auth.json")
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
	if !isConfigPath(path) {
		return defaultVal, fmt.Errorf("%w: %s", ErrInvalidConfigPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultVal, nil
		}
		return defaultVal, fmt.Errorf("read config file: %w", err)
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return defaultVal, fmt.Errorf("decode config: %w", err)
	}
	return result, nil
}

func saveJSON[T any](pathFunc func() (string, error), value T) error {
	path, err := pathFunc()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := ensureConfigDir(); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func configPath(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func ensureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return nil
}

func isConfigPath(path string) bool {
	if path == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	configDir, err := ConfigDir()
	if err != nil {
		return false
	}
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absConfigDir, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, "..")
}

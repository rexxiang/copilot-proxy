package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configFileMode = 0o600
	configDirMode  = 0o700
)

var (
	ErrInvalidPath = errors.New("invalid config path")
)

// ConfigDir returns the runtime config base directory.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".config", "copilot-proxy"), nil
}

// Path returns a file path under the runtime config directory.
func Path(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// IsConfigPath reports whether path is inside the runtime config directory.
func IsConfigPath(path string) bool {
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

// LoadJSON reads a JSON file and returns defaultVal when the file does not exist.
func LoadJSON[T any](path string, defaultVal T) (T, error) {
	if !IsConfigPath(path) {
		return defaultVal, fmt.Errorf("%w: %s", ErrInvalidPath, path)
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

// SaveJSON writes a JSON file under the runtime config directory.
func SaveJSON[T any](path string, value T) error {
	if !IsConfigPath(path) {
		return fmt.Errorf("%w: %s", ErrInvalidPath, path)
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

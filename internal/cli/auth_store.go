package cli

import (
	"fmt"
	"sync"

	"copilot-proxy/internal/config"
)

type authStore struct {
	mu   sync.Mutex
	auth config.AuthConfig
}

func newAuthStore(auth config.AuthConfig) *authStore {
	return &authStore{
		mu:   sync.Mutex{},
		auth: cloneAuth(auth),
	}
}

func (s *authStore) LoadAuth() (config.AuthConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAuth(s.auth), nil
}

func (s *authStore) SaveAuth(auth config.AuthConfig) error {
	if err := config.SaveAuth(auth); err != nil {
		return fmt.Errorf("save auth config: %w", err)
	}
	s.mu.Lock()
	s.auth = cloneAuth(auth)
	s.mu.Unlock()
	return nil
}

func cloneAuth(auth config.AuthConfig) config.AuthConfig {
	accounts := make([]config.Account, len(auth.Accounts))
	copy(accounts, auth.Accounts)
	auth.Accounts = accounts
	return auth
}

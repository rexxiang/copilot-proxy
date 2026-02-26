package cli

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"copilot-proxy/internal/config"
)

var (
	errRuntimeSwitcherRequired = errors.New("runtime switcher is required")
	errPersistSettingsRequired = errors.New("persist settings callback is required")
	errRollbackRuntimeRequired = errors.New("rollback runtime callback is required")
	errCandidateSettingsNil    = errors.New("candidate settings is nil")
)

type RuntimeCoordinatorConfig struct {
	InitialSettings config.Settings
	SwitchRuntime   func(prev, next config.Settings) error
	PersistSettings func(settings config.Settings) error
	RollbackRuntime func(settings config.Settings) error
}

type SettingsRuntimeCoordinator struct {
	mu              sync.Mutex
	current         config.Settings
	switchRuntime   func(prev, next config.Settings) error
	persistSettings func(settings config.Settings) error
	rollbackRuntime func(settings config.Settings) error
}

func NewSettingsRuntimeCoordinator(cfg *RuntimeCoordinatorConfig) *SettingsRuntimeCoordinator {
	defaultSettings := config.DefaultSettings()
	if cfg == nil {
		return &SettingsRuntimeCoordinator{
			mu:              sync.Mutex{},
			current:         defaultSettings,
			switchRuntime:   nil,
			persistSettings: nil,
			rollbackRuntime: nil,
		}
	}
	return &SettingsRuntimeCoordinator{
		mu:              sync.Mutex{},
		current:         cfg.InitialSettings,
		switchRuntime:   cfg.SwitchRuntime,
		persistSettings: cfg.PersistSettings,
		rollbackRuntime: cfg.RollbackRuntime,
	}
}

func (c *SettingsRuntimeCoordinator) Current() config.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *SettingsRuntimeCoordinator) Apply(candidate *config.Settings) (config.Settings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.switchRuntime == nil {
		return c.current, errRuntimeSwitcherRequired
	}
	if c.persistSettings == nil {
		return c.current, errPersistSettingsRequired
	}
	if c.rollbackRuntime == nil {
		return c.current, errRollbackRuntimeRequired
	}
	if candidate == nil {
		return c.current, errCandidateSettingsNil
	}

	previous := c.current
	if reflect.DeepEqual(previous, *candidate) {
		return previous, nil
	}

	if err := c.switchRuntime(previous, *candidate); err != nil {
		return previous, fmt.Errorf("switch runtime: %w", err)
	}

	if err := c.persistSettings(*candidate); err != nil {
		rollbackErr := c.rollbackRuntime(previous)
		if rollbackErr != nil {
			return previous, errors.Join(
				fmt.Errorf("persist settings: %w", err),
				fmt.Errorf("rollback runtime: %w", rollbackErr),
			)
		}
		return previous, fmt.Errorf("persist settings: %w", err)
	}

	c.current = *candidate
	return c.current, nil
}

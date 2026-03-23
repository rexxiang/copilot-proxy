package app

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
)

var (
	errRuntimeValidatorRequired = errors.New("runtime validator is required")
	errPersistSettingsRequired  = errors.New("persist settings callback is required")
	errRuntimePublisherRequired = errors.New("runtime publisher is required")
	errCandidateSettingsNil     = errors.New("candidate settings is nil")
)

type RuntimeValidationResult any

type RuntimeCoordinatorConfig struct {
	InitialSettings appsettings.Settings
	ValidateRuntime func(next appsettings.Settings) (RuntimeValidationResult, error)
	PersistSettings func(settings appsettings.Settings) error
	PublishRuntime  func(next appsettings.Settings, validated RuntimeValidationResult) error
}

type SettingsRuntimeCoordinator struct {
	mu              sync.Mutex
	current         appsettings.Settings
	validateRuntime func(next appsettings.Settings) (RuntimeValidationResult, error)
	persistSettings func(settings appsettings.Settings) error
	publishRuntime  func(next appsettings.Settings, validated RuntimeValidationResult) error
}

func NewSettingsRuntimeCoordinator(cfg *RuntimeCoordinatorConfig) *SettingsRuntimeCoordinator {
	defaultSettings := appsettings.DefaultSettings()
	if cfg == nil {
		return &SettingsRuntimeCoordinator{
			mu:              sync.Mutex{},
			current:         defaultSettings,
			validateRuntime: nil,
			persistSettings: nil,
			publishRuntime:  nil,
		}
	}
	return &SettingsRuntimeCoordinator{
		mu:              sync.Mutex{},
		current:         cfg.InitialSettings,
		validateRuntime: cfg.ValidateRuntime,
		persistSettings: cfg.PersistSettings,
		publishRuntime:  cfg.PublishRuntime,
	}
}

func (c *SettingsRuntimeCoordinator) Current() appsettings.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *SettingsRuntimeCoordinator) Apply(candidate *appsettings.Settings) (appsettings.Settings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.validateRuntime == nil {
		return c.current, errRuntimeValidatorRequired
	}
	if c.persistSettings == nil {
		return c.current, errPersistSettingsRequired
	}
	if c.publishRuntime == nil {
		return c.current, errRuntimePublisherRequired
	}
	if candidate == nil {
		return c.current, errCandidateSettingsNil
	}

	previous := c.current
	if reflect.DeepEqual(previous, *candidate) {
		return previous, nil
	}

	validated, err := c.validateRuntime(*candidate)
	if err != nil {
		return previous, fmt.Errorf("validate runtime: %w", err)
	}

	if err := c.persistSettings(*candidate); err != nil {
		return previous, fmt.Errorf("persist settings: %w", err)
	}

	if err := c.publishRuntime(*candidate, validated); err != nil {
		return previous, errors.Join(
			fmt.Errorf("publish runtime: %w", err),
			fmt.Errorf("persisted settings may require manual rollback"),
		)
	}

	c.current = *candidate
	return c.current, nil
}

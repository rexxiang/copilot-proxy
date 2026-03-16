package controller

import (
	"context"
	"errors"
	"fmt"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/account"
	coreconfig "copilot-proxy/internal/core/config"
	"copilot-proxy/internal/core/kernel"
	coremodel "copilot-proxy/internal/core/model"
	"copilot-proxy/internal/core/observability"
	"copilot-proxy/internal/core/runtime"
	corestats "copilot-proxy/internal/core/stats"
	"copilot-proxy/internal/models"
)

const (
	defaultCollectorMax = 1000
)

// ControllerDeps configure the controller creation.
type ControllerDeps struct {
	Runtime             runtime.RuntimeDeps
	Collector           *observability.Collector
	PersistentCollector *observability.PersistentCollector
}

// ServiceController orchestrates the kernel, runtime, and core services.
type ServiceController struct {
	runtime    *runtime.Runtime
	kernel     *kernel.Kernel
	collector  *observability.Collector
	persistent *observability.PersistentCollector
	account    *account.Service
	config     *coreconfig.Service
	model      *coremodel.Service
	stats      *corestats.Service
}

// NewServiceController builds the controller with the provided dependencies.
func NewServiceController(ctx context.Context, deps ControllerDeps) (*ServiceController, error) {
	collector := deps.Collector
	if collector == nil {
		collector = observability.NewCollector(defaultCollectorMax)
	}

	if deps.Runtime.ModelCatalog == nil {
		deps.Runtime.ModelCatalog = models.DefaultModelsManager()
	}

	if deps.Runtime.SettingsFunc == nil {
		deps.Runtime.SettingsFunc = config.LoadSettings
	}
	if deps.Runtime.AuthFunc == nil {
		deps.Runtime.AuthFunc = config.LoadAuth
	}

	if deps.Runtime.Observability == nil {
		deps.Runtime.Observability = collector
	}

	rt, err := runtime.NewRuntimeWithContext(ctx, deps.Runtime)
	if err != nil {
		return nil, fmt.Errorf("build runtime: %w", err)
	}

	obs := collector.Observability()
	if obs == nil {
		return nil, errors.New("collector missing observability")
	}

	kern := kernel.NewKernel(rt, obs)
	proxyAddr := rt.Settings.ListenAddr
	if proxyAddr == "" {
		proxyAddr = config.DefaultSettings().ListenAddr
	}
	modelProxy := "http://" + proxyAddr

	return &ServiceController{
		runtime:    rt,
		kernel:     kern,
		collector:  collector,
		persistent: deps.PersistentCollector,
		account:    account.New(rt.Auth),
		config:     coreconfig.NewService(rt.Settings),
		model:      coremodel.NewService(rt.ModelCatalog, deps.Runtime.ModelLoader, deps.Runtime.HTTPClient, modelProxy),
		stats:      corestats.NewService(obs),
	}, nil
}

// Start runs the kernel and blocks until the server stops.
func (c *ServiceController) Start() error {
	return c.kernel.Start()
}

// Stop shuts down the kernel/server.
func (c *ServiceController) Stop() error {
	return c.kernel.Stop()
}

// Status reports the kernel state.
func (c *ServiceController) Status() core.ServiceState {
	return c.kernel.Status()
}

// Invoke runs an in-process request through the kernel.
func (c *ServiceController) Invoke(r core.RequestInvocation) (core.ResponsePayload, error) {
	return c.kernel.Invoke(r)
}

// Runtime returns the underlying runtime.
func (c *ServiceController) Runtime() *runtime.Runtime {
	return c.runtime
}

// Collector exposes the thread-safe collector backing metrics.
func (c *ServiceController) Collector() *observability.Collector {
	return c.collector
}

// PersistentCollector returns the persistent collector when available.
func (c *ServiceController) PersistentCollector() *observability.PersistentCollector {
	return c.persistent
}

// AccountService returns the account service.
func (c *ServiceController) AccountService() *account.Service {
	return c.account
}

// ConfigService returns the config service.
func (c *ServiceController) ConfigService() *coreconfig.Service {
	return c.config
}

// ModelService returns the model service.
func (c *ServiceController) ModelService() *coremodel.Service {
	return c.model
}

// StatsService returns the stats service.
func (c *ServiceController) StatsService() *corestats.Service {
	return c.stats
}

// AuthConfig returns the current auth configuration snapshot.
func (c *ServiceController) AuthConfig() config.AuthConfig {
	return c.runtime.Auth
}

// Settings returns the current settings snapshot.
func (c *ServiceController) Settings() config.Settings {
	return c.runtime.Settings
}

package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	model "copilot-proxy/internal/runtime/model"
	"copilot-proxy/internal/runtime/observability"
	runtime "copilot-proxy/internal/runtime/server"
	corestats "copilot-proxy/internal/runtime/stats"
	core "copilot-proxy/internal/runtime/types"
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

// ServiceController orchestrates runtime lifecycle and core services.
type ServiceController struct {
	mu                sync.Mutex
	startCtx          context.Context
	state             core.ServiceState
	host              *serverHost
	runtime           *runtime.Runtime
	collector         *observability.Collector
	persistent        *observability.PersistentCollector
	model             *model.Service
	stats             *corestats.Service
	obs               *observability.Observability
	stopEventReported bool
}

// NewServiceController builds the controller with the provided dependencies.
func NewServiceController(ctx context.Context, deps ControllerDeps) (*ServiceController, error) {
	collector := deps.Collector
	if collector == nil {
		collector = observability.NewCollector(defaultCollectorMax)
	}

	if deps.Runtime.ModelCatalog == nil {
		deps.Runtime.ModelCatalog = model.NewManager()
	}

	if deps.Runtime.SettingsFunc == nil {
		deps.Runtime.SettingsFunc = loadRuntimeConfigFromAppSettings
	}
	if deps.Runtime.AuthFunc == nil {
		deps.Runtime.AuthFunc = runtimeconfig.LoadAuth
	}

	if deps.Runtime.Observability == nil {
		deps.Runtime.Observability = collector
	}

	rt, err := runtime.NewRuntimeWithContext(ctx, deps.Runtime)
	if err != nil {
		return nil, fmt.Errorf("build runtime: %w", err)
	}
	if rt == nil || rt.Handler == nil {
		return nil, errors.New("runtime handler is not configured")
	}

	obs := collector.Observability()
	if obs == nil {
		return nil, errors.New("collector missing observability")
	}

	modelLoader := deps.Runtime.ModelLoader
	if modelLoader == nil {
		modelLoader = runtimeModelLoader{runtime: rt}
	}

	listenAddr := rt.ListenAddr
	if listenAddr == "" {
		listenAddr = runtimeconfig.Default().ListenAddr
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return &ServiceController{
		startCtx:   ctx,
		state:      core.StateStopped,
		host:       newServerHost(listenAddr, rt.Handler),
		runtime:    rt,
		collector:  collector,
		persistent: deps.PersistentCollector,
		model:      model.NewService(deps.Runtime.ModelCatalog, modelLoader, deps.Runtime.HTTPClient, ""),
		stats:      corestats.NewService(obs),
		obs:        obs,
	}, nil
}

// Start runs the controller lifecycle and blocks until the server stops.
func (c *ServiceController) Start() error {
	c.mu.Lock()
	if c.state == core.StateRunning {
		c.mu.Unlock()
		return nil
	}
	if c.host == nil {
		c.mu.Unlock()
		return errors.New("runtime server host is not configured")
	}
	c.state = core.StateRunning
	c.stopEventReported = false
	c.addEventLocked(core.KernelEventTypeStart, "kernel started")
	host := c.host
	startCtx := c.startCtx
	c.mu.Unlock()

	if startCtx == nil {
		startCtx = context.Background()
	}
	err := host.Start(startCtx)

	c.mu.Lock()
	c.state = core.StateStopped
	c.recordStopEventLocked()
	c.mu.Unlock()
	if err != nil && !isExpectedShutdownError(err) {
		return err
	}
	return nil
}

// Stop shuts down the server host and runtime resources.
func (c *ServiceController) Stop() error {
	c.mu.Lock()
	if c.state == core.StateStopped {
		c.mu.Unlock()
		return nil
	}
	host := c.host
	var reqCloser interface{ Close() error }
	if c.runtime != nil {
		reqCloser = c.runtime.RequestCloser
	}
	c.mu.Unlock()

	if host != nil {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeconfig.ShutdownTimeout)
		defer cancel()
		_ = host.Shutdown(ctx)
		_ = host.Close()
	}
	if reqCloser != nil {
		_ = reqCloser.Close()
	}

	c.mu.Lock()
	c.state = core.StateStopped
	c.recordStopEventLocked()
	c.mu.Unlock()
	return nil
}

// Status reports the service state.
func (c *ServiceController) Status() core.ServiceState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
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

// ModelService returns the model service.
func (c *ServiceController) ModelService() *model.Service {
	return c.model
}

// StatsService returns the stats service.
func (c *ServiceController) StatsService() *corestats.Service {
	return c.stats
}

func (c *ServiceController) addEventLocked(eventType, message string) {
	if c.obs == nil {
		return
	}
	c.obs.AddEvent(observability.Event{Timestamp: time.Now(), Type: eventType, Message: message})
}

func (c *ServiceController) recordStopEventLocked() {
	if c.stopEventReported {
		return
	}
	c.addEventLocked(core.KernelEventTypeStop, "kernel stopped")
	c.stopEventReported = true
}

type runtimeModelLoader struct {
	runtime *runtime.Runtime
}

func (l runtimeModelLoader) Load(ctx context.Context) ([]model.ModelInfo, error) {
	if l.runtime == nil {
		return nil, errors.New("runtime model loader is not configured")
	}
	return l.runtime.RefreshModels(ctx, "")
}

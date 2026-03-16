package kernel

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/observability"
	coreRuntime "copilot-proxy/internal/core/runtime"
)

// Kernel orchestrates core services and collects observability data.
type Kernel struct {
	mu                sync.Mutex
	obs               *observability.Observability
	state             core.ServiceState
	runtime           *coreRuntime.Runtime
	stopEventReported bool
}

// NewKernel initializes the kernel with the provided observability collector.
func NewKernel(runtime *coreRuntime.Runtime, obs *observability.Observability) *Kernel {
	if obs == nil {
		obs = observability.New(100, 200)
	}
	return &Kernel{runtime: runtime, obs: obs, state: core.StateStopped}
}

// Start transitions the kernel to running state.
func (k *Kernel) Start() error {
	k.mu.Lock()
	if k.state == core.StateRunning {
		k.mu.Unlock()
		return nil
	}
	if k.runtime == nil || k.runtime.Server == nil {
		k.mu.Unlock()
		return errors.New("core: runtime server not configured")
	}
	k.state = core.StateRunning
	k.stopEventReported = false
	k.obs.AddEvent(observability.Event{Timestamp: time.Now(), Type: core.KernelEventTypeStart, Message: "kernel started"})
	server := k.runtime.Server
	k.mu.Unlock()

	err := server.Start(context.Background())
	k.mu.Lock()
	k.state = core.StateStopped
	k.recordStopEventLocked()
	k.mu.Unlock()
	if err != nil && !isExpectedShutdownError(err) {
		return err
	}
	return nil
}

// Stop transitions the kernel to stopped state.
func (k *Kernel) Stop() error {
	k.mu.Lock()
	if k.state == core.StateStopped {
		k.mu.Unlock()
		return nil
	}
	server := k.runtime.Server
	reqCloser := k.runtime.RequestCloser
	k.mu.Unlock()

	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = server.Close()
	}
	if reqCloser != nil {
		_ = reqCloser.Close()
	}

	k.mu.Lock()
	k.state = core.StateStopped
	k.recordStopEventLocked()
	k.mu.Unlock()
	return nil
}

// Status returns the kernel state.
func (k *Kernel) Status() core.ServiceState {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.state
}

// Invoke runs an in-process request (stubbed for now).
func (k *Kernel) Invoke(r core.RequestInvocation) (core.ResponsePayload, error) {
	if r.Method == "" || r.Path == "" {
		return core.ResponsePayload{}, core.ErrInvalidInvocation
	}
	if k.Status() != core.StateRunning {
		return core.ResponsePayload{}, core.ErrNotStarted
	}
	k.obs.AddEvent(observability.Event{
		Timestamp: time.Now(),
		Type:      core.KernelEventTypeInvoke,
		Message:   "in-process invocation",
		Payload: map[string]any{
			"method": r.Method,
			"path":   r.Path,
		},
	})
	return core.ResponsePayload{StatusCode: http.StatusNotImplemented, Headers: nil, Body: nil}, nil
}

func isExpectedShutdownError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed)
}

func (k *Kernel) recordStopEventLocked() {
	if k.stopEventReported {
		return
	}
	k.obs.AddEvent(observability.Event{Timestamp: time.Now(), Type: core.KernelEventTypeStop, Message: "kernel stopped"})
	k.stopEventReported = true
}

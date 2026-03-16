package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/kernel"
	"copilot-proxy/internal/core/observability"
	coreRuntime "copilot-proxy/internal/core/runtime"
	"copilot-proxy/internal/models"
)

const (
	defaultQueueSize        = 64
	kernelStartTimeout      = 5 * time.Second
	kernelStatePollInterval = 10 * time.Millisecond
)

var (
	errCoreDestroyed     = errors.New("copilot-proxy-core: destroyed")
	errKernelNotRunning  = core.ErrNotStarted
	errKernelUnavailable = errors.New("copilot-proxy-core: kernel unavailable")
)

type invocationTask struct {
	payload string
	ack     chan error
	id      uint64
}

type copilotProxyCore struct {
	kernel  *kernel.Kernel
	initErr error
	queue   chan *invocationTask
	destroy chan struct{}
	obs     *observability.Observability

	serverMu    sync.Mutex
	serverDone  chan struct{}
	serverErr   error
	serverErrMu sync.Mutex
	serverWG    sync.WaitGroup

	callbackMu sync.RWMutex
	callback   func(string, error, uint64)

	idCounter atomic.Uint64
	destroyed atomic.Bool
	once      sync.Once
	wg        sync.WaitGroup
	invoking  sync.WaitGroup
}

func newCopilotProxyCore() *copilotProxyCore {
	obs := observability.New(100, 0)
	kern, err := buildKernel(obs)
	core := &copilotProxyCore{
		kernel:  kern,
		initErr: err,
		queue:   make(chan *invocationTask, defaultQueueSize),
		destroy: make(chan struct{}),
		obs:     obs,
	}
	core.wg.Add(1)
	go core.run()
	return core
}

func buildKernel(obs *observability.Observability) (*kernel.Kernel, error) {
	if obs == nil {
		obs = observability.New(100, 0)
	}
	rt, err := coreRuntime.NewRuntime(coreRuntime.RuntimeDeps{
		SettingsFunc: func() (config.Settings, error) {
			settings := config.DefaultSettings()
			settings.ListenAddr = "127.0.0.1:0"
			return settings, nil
		},
		ModelLoader: noopModelLoader{},
	})
	if err != nil {
		return nil, fmt.Errorf("build runtime: %w", err)
	}
	return kernel.NewKernel(rt, obs), nil
}

func (c *copilotProxyCore) run() {
	defer c.wg.Done()
	for {
		select {
		case task := <-c.queue:
			c.handleTask(task)
		case <-c.destroy:
			c.drainQueue()
			return
		}
	}
}

func (c *copilotProxyCore) drainQueue() {
	for {
		select {
		case task := <-c.queue:
			if task == nil {
				continue
			}
			c.handleTask(task)
		default:
			return
		}
	}
}

func (c *copilotProxyCore) handleTask(task *invocationTask) {
	if task == nil {
		return
	}
	var request core.RequestInvocation
	if err := json.Unmarshal([]byte(task.payload), &request); err != nil {
		c.finishTask(task, "", fmt.Errorf("invalid request: %w", err))
		return
	}
	if err := c.ensureKernel(); err != nil {
		c.finishTask(task, "", err)
		return
	}
	if c.kernel.Status() != core.StateRunning {
		c.finishTask(task, "", errKernelNotRunning)
		return
	}
	response, err := c.kernel.Invoke(request)
	if err != nil {
		c.finishTask(task, "", err)
		return
	}
	payload, err := json.Marshal(response)
	if err != nil {
		c.finishTask(task, "", fmt.Errorf("serializing response: %w", err))
		return
	}
	c.finishTask(task, string(payload), nil)
}

func (c *copilotProxyCore) finishTask(task *invocationTask, payload string, err error) {
	if task == nil {
		return
	}
	task.ack <- err
	c.invokeCallback(payload, err, task.id)
}

func (c *copilotProxyCore) Start() error {
	if c.destroyed.Load() {
		return errCoreDestroyed
	}
	if err := c.ensureKernel(); err != nil {
		return err
	}

	c.serverMu.Lock()
	if c.kernel.Status() == core.StateRunning {
		c.serverMu.Unlock()
		return nil
	}
	done := make(chan struct{})
	c.serverDone = done
	c.resetServerErr()
	c.serverWG.Add(1)
	go c.runKernel(done)
	c.serverMu.Unlock()

	if err := c.waitForState(core.StateRunning, done); err != nil {
		return err
	}
	c.logEvent("kernel.start", "kernel started (C ABI)")
	return nil
}

func (c *copilotProxyCore) Stop() error {
	if c.destroyed.Load() {
		return errCoreDestroyed
	}
	if err := c.ensureKernel(); err != nil {
		return err
	}

	c.serverMu.Lock()
	done := c.serverDone
	c.serverMu.Unlock()
	if done == nil {
		return nil
	}

	if err := c.kernel.Stop(); err != nil {
		return err
	}
	<-done

	c.serverMu.Lock()
	c.serverDone = nil
	c.serverMu.Unlock()

	c.logEvent("kernel.stop", "kernel stopped (C ABI)")
	return c.serverError()
}

func (c *copilotProxyCore) Status() core.ServiceState {
	if c.kernel == nil {
		return core.StateStopped
	}
	return c.kernel.Status()
}

func (c *copilotProxyCore) Invoke(payload string) error {
	if c.destroyed.Load() {
		return errCoreDestroyed
	}
	if err := c.ensureKernel(); err != nil {
		return err
	}
	c.invoking.Add(1)
	defer c.invoking.Done()
	if c.destroyed.Load() {
		return errCoreDestroyed
	}
	if c.kernel.Status() != core.StateRunning {
		return errKernelNotRunning
	}

	task := &invocationTask{
		payload: payload,
		ack:     make(chan error, 1),
		id:      c.idCounter.Add(1),
	}

	select {
	case c.queue <- task:
	case <-c.destroy:
		return errCoreDestroyed
	}

	select {
	case err := <-task.ack:
		return err
	case <-c.destroy:
		return errCoreDestroyed
	}
}

func (c *copilotProxyCore) Destroy() {
	c.once.Do(func() {
		c.destroyed.Store(true)
		_ = c.Stop()
		c.invoking.Wait()
		close(c.destroy)
		c.wg.Wait()
		c.serverWG.Wait()
	})
}

func (c *copilotProxyCore) SetCallback(fn func(string, error, uint64)) {
	c.callbackMu.Lock()
	c.callback = fn
	c.callbackMu.Unlock()
}

func (c *copilotProxyCore) invokeCallback(payload string, err error, id uint64) {
	c.callbackMu.RLock()
	cb := c.callback
	c.callbackMu.RUnlock()
	if cb == nil {
		return
	}
	cb(payload, err, id)
}

func (c *copilotProxyCore) waitForState(target core.ServiceState, done <-chan struct{}) error {
	deadline := time.Now().Add(kernelStartTimeout)
	for {
		if c.kernel.Status() == target {
			return nil
		}
		select {
		case <-done:
			if err := c.serverError(); err != nil {
				return err
			}
			if c.kernel.Status() == target {
				return nil
			}
			return fmt.Errorf("kernel terminated before reaching %s", target)
		default:
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(kernelStatePollInterval)
	}
	if c.kernel.Status() == target {
		return nil
	}
	return fmt.Errorf("timeout waiting for kernel to reach %s", target)
}

func (c *copilotProxyCore) runKernel(done chan struct{}) {
	defer c.serverWG.Done()
	err := c.kernel.Start()
	c.setServerErr(err)
	close(done)
}

func (c *copilotProxyCore) setServerErr(err error) {
	c.serverErrMu.Lock()
	c.serverErr = err
	c.serverErrMu.Unlock()
}

func (c *copilotProxyCore) resetServerErr() {
	c.serverErrMu.Lock()
	c.serverErr = nil
	c.serverErrMu.Unlock()
}

func (c *copilotProxyCore) serverError() error {
	c.serverErrMu.Lock()
	err := c.serverErr
	c.serverErrMu.Unlock()
	return err
}

func (c *copilotProxyCore) ensureKernel() error {
	if c.initErr != nil {
		return c.initErr
	}
	if c.kernel == nil {
		return errKernelUnavailable
	}
	return nil
}

func (c *copilotProxyCore) logEvent(eventType, message string) {
	if c.obs == nil {
		return
	}
	c.obs.AddEvent(observability.Event{Timestamp: time.Now(), Type: eventType, Message: message})
}

type noopModelLoader struct{}

func (noopModelLoader) Load(context.Context) ([]models.ModelInfo, error) {
	return nil, nil
}

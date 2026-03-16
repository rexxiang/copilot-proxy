package kernel

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/observability"
	coreRuntime "copilot-proxy/internal/core/runtime"
	"copilot-proxy/internal/server"
)

func TestKernelStartStopLifecycle(t *testing.T) {
	obs := observability.New(10, 10)
	k := NewKernel(newTestRuntime(t), obs)

	if got := k.Status(); got != core.StateStopped {
		t.Fatalf("initial state = %s, want %s", got, core.StateStopped)
	}
	errCh := startKernelAsync(t, k)

	if got := k.Status(); got != core.StateRunning {
		t.Fatalf("state after start = %s, want %s", got, core.StateRunning)
	}

	events := obs.Events()
	if len(events) == 0 || events[0].Type != core.KernelEventTypeStart {
		t.Fatalf("expected kernel.start event, got %+v", events)
	}

	if err := k.Start(); err != nil {
		t.Fatalf("second start should be no-op: %v", err)
	}

	if got := k.Status(); got != core.StateRunning {
		t.Fatalf("state should remain running after redundant start, got %s", got)
	}

	stopKernelAndWait(t, k, errCh)
	if got := k.Status(); got != core.StateStopped {
		t.Fatalf("state after stop = %s, want %s", got, core.StateStopped)
	}
	if err := k.Stop(); err != nil {
		t.Fatalf("second stop should be no-op: %v", err)
	}

	events = obs.Events()
	if len(events) < 2 || events[len(events)-1].Type != core.KernelEventTypeStop {
		t.Fatalf("expected kernel.stop event, got %+v", events)
	}
}

func TestKernelInvokeFlow(t *testing.T) {
	obs := observability.New(5, 5)
	k := NewKernel(newTestRuntime(t), obs)

	if _, err := k.Invoke(core.RequestInvocation{Method: http.MethodGet, Path: "/test"}); !errors.Is(err, core.ErrNotStarted) {
		t.Fatalf("invoke should fail when kernel not running: %v", err)
	}

	errCh := startKernelAsync(t, k)
	defer stopKernelAndWait(t, k, errCh)

	if _, err := k.Invoke(core.RequestInvocation{Method: "", Path: "/missing"}); !errors.Is(err, core.ErrInvalidInvocation) {
		t.Fatalf("invoke should fail when method missing: %v", err)
	}

	resp, err := k.Invoke(core.RequestInvocation{Method: http.MethodPost, Path: "/ok"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}

	found := false
	for _, ev := range obs.Events() {
		if ev.Type == core.KernelEventTypeInvoke {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected kernel.invoke event, saw %v", obs.Events())
	}
}

func newTestRuntime(t *testing.T) *coreRuntime.Runtime {
	t.Helper()
	settings := config.DefaultSettings()
	settings.ListenAddr = "127.0.0.1:0"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := server.New(&settings, handler)
	return &coreRuntime.Runtime{Server: srv}
}

func startKernelAsync(t *testing.T, k *Kernel) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- k.Start()
	}()
	waitForCondition(t, func() bool { return k.Status() == core.StateRunning }, "kernel start")
	return errCh
}

func stopKernelAndWait(t *testing.T, k *Kernel, errCh <-chan error) {
	t.Helper()
	if err := k.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for kernel to stop")
	}
}

func waitForCondition(t *testing.T, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", desc)
}

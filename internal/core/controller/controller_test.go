package controller_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/core/controller"
	"copilot-proxy/internal/core/observability"
	"copilot-proxy/internal/core/runtime"
	"copilot-proxy/internal/models"
)

type testLoader struct {
	models []models.ModelInfo
}

func (l testLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return l.models, nil
}

func newTestController(t *testing.T) *controller.ServiceController {
	return buildTestController(t, nil)
}

func buildTestController(t *testing.T, modify func(*controller.ControllerDeps)) *controller.ServiceController {
	t.Helper()
	ctx := context.Background()
	deps := controller.ControllerDeps{
		Runtime: runtime.RuntimeDeps{
			SettingsFunc: func() (config.Settings, error) {
				settings := config.DefaultSettings()
				settings.ListenAddr = "127.0.0.1:0"
				return settings, nil
			},
			AuthFunc: func() (config.AuthConfig, error) {
				return config.AuthConfig{}, nil
			},
			ModelCatalog: models.DefaultModelsManager(),
			ModelLoader:  testLoader{models: []models.ModelInfo{{ID: "test-model"}}},
		},
	}
	if modify != nil {
		modify(&deps)
	}
	ctrl, err := controller.NewServiceController(ctx, deps)
	if err != nil {
		t.Fatalf("build controller: %v", err)
	}
	return ctrl
}

func TestServiceControllerLifecycle(t *testing.T) {
	ctrl := newTestController(t)
	if ctrl.AccountService() == nil {
		t.Fatal("account service missing")
	}
	if ctrl.ConfigService() == nil {
		t.Fatal("config service missing")
	}
	if ctrl.ModelService() == nil {
		t.Fatal("model service missing")
	}
	models := ctrl.ModelService().List()
	if len(models) == 0 || models[0].ID != "test-model" {
		t.Fatalf("unexpected models: %v", models)
	}
	snapshot := ctrl.StatsService().MonitorSnapshot()
	if snapshot.TotalRequests != 0 {
		t.Fatalf("expected zero requests, got %d", snapshot.TotalRequests)
	}
	if got := ctrl.Status(); got != core.StateStopped {
		t.Fatalf("initial state = %s, want %s", got, core.StateStopped)
	}
	errCh := startControllerAsync(t, ctrl)
	if got := ctrl.Status(); got != core.StateRunning {
		t.Fatalf("state after start = %s, want %s", got, core.StateRunning)
	}
	stopControllerAndWait(t, ctrl, errCh)
	if got := ctrl.Status(); got != core.StateStopped {
		t.Fatalf("state after stop = %s, want %s", got, core.StateStopped)
	}
}

func TestServiceControllerInvoke(t *testing.T) {
	ctrl := newTestController(t)
	if _, err := ctrl.Invoke(core.RequestInvocation{Method: http.MethodGet, Path: "/test"}); err == nil {
		t.Fatalf("expected error when invoking before start")
	}
	errCh := startControllerAsync(t, ctrl)
	defer stopControllerAndWait(t, ctrl, errCh)
	resp, err := ctrl.Invoke(core.RequestInvocation{Method: http.MethodPost, Path: "/ok"})
	if err != nil {
		t.Fatalf("invoke failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestServiceControllerStartStopIdempotent(t *testing.T) {
	ctrl := newTestController(t)
	errCh := startControllerAsync(t, ctrl)
	if err := ctrl.Start(); err != nil {
		t.Fatalf("expected second start to be a no-op: %v", err)
	}
	if ctrl.Status() != core.StateRunning {
		t.Fatalf("expected controller running after duplicate start, got %s", ctrl.Status())
	}
	stopControllerAndWait(t, ctrl, errCh)
	if err := ctrl.Stop(); err != nil {
		t.Fatalf("expected second stop to succeed, got %v", err)
	}
}

func TestServiceControllerCollectorInjection(t *testing.T) {
	custom := observability.NewCollector(5)
	ctrl := buildTestController(t, func(deps *controller.ControllerDeps) {
		deps.Collector = custom
	})
	if got := ctrl.Collector(); got != custom {
		t.Fatalf("collector mismatch, want %p got %p", custom, got)
	}
}

func TestServiceControllerPersistentCollectorExposure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	persistent := observability.NewPersistentCollector(5, path)
	ctrl := buildTestController(t, func(deps *controller.ControllerDeps) {
		deps.PersistentCollector = persistent
	})
	if got := ctrl.PersistentCollector(); got != persistent {
		t.Fatalf("persistent collector mismatch, want %p got %p", persistent, got)
	}
}

func startControllerAsync(t *testing.T, ctrl *controller.ServiceController) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Start()
	}()
	waitForCondition(t, func() bool { return ctrl.Status() == core.StateRunning }, "controller start")
	return errCh
}

func stopControllerAndWait(t *testing.T, ctrl *controller.ServiceController, errCh <-chan error) {
	t.Helper()
	if err := ctrl.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for controller to stop")
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

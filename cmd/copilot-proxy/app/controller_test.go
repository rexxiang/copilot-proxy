package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
	"copilot-proxy/internal/runtime/observability"
	runtime "copilot-proxy/internal/runtime/server"
	core "copilot-proxy/internal/runtime/types"
)

type testLoader struct {
	models []models.ModelInfo
}

func (l testLoader) Load(ctx context.Context) ([]models.ModelInfo, error) {
	return l.models, nil
}

func newTestController(t *testing.T) *ServiceController {
	return buildTestController(t, nil)
}

func buildTestController(t *testing.T, modify func(*ControllerDeps)) *ServiceController {
	t.Helper()
	ctx := context.Background()
	deps := ControllerDeps{
		Runtime: runtime.RuntimeDeps{
			SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
				settings := appsettings.ToRuntimeConfig(appsettings.DefaultSettings())
				settings.ListenAddr = "127.0.0.1:0"
				return settings, nil
			},
			AuthFunc: func() (runtimeconfig.AuthConfig, error) {
				return runtimeconfig.AuthConfig{}, nil
			},
			ModelCatalog: models.NewManager(),
			ModelLoader:  testLoader{models: []models.ModelInfo{{ID: "test-model"}}},
		},
	}
	if modify != nil {
		modify(&deps)
	}
	ctrl, err := NewServiceController(ctx, deps)
	if err != nil {
		t.Fatalf("build controller: %v", err)
	}
	return ctrl
}

func TestServiceControllerLifecycle(t *testing.T) {
	ctrl := newTestController(t)
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
	ctrl := buildTestController(t, func(deps *ControllerDeps) {
		deps.Collector = custom
	})
	if got := ctrl.Collector(); got != custom {
		t.Fatalf("collector mismatch, want %p got %p", custom, got)
	}
}

func TestServiceControllerPersistentCollectorExposure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	persistent := observability.NewPersistentCollector(5, path)
	ctrl := buildTestController(t, func(deps *ControllerDeps) {
		deps.PersistentCollector = persistent
	})
	if got := ctrl.PersistentCollector(); got != persistent {
		t.Fatalf("persistent collector mismatch, want %p got %p", persistent, got)
	}
}

func TestServiceControllerModelRefreshUsesCorePathWithoutLocalServer(t *testing.T) {
	var (
		authHeader string
		path       string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                   "gpt-5-mini",
					"name":                 "GPT-5 Mini",
					"vendor":               "OpenAI",
					"version":              "1",
					"preview":              false,
					"model_picker_enabled": true,
					"supported_endpoints":  []string{runtimeconfig.UpstreamChatCompletionsPath},
					"billing": map[string]any{
						"is_premium": false,
						"multiplier": 1.0,
					},
					"capabilities": map[string]any{
						"family": "gpt-5-mini",
						"type":   "chat",
						"supports": map[string]any{
							"reasoning_effort": []string{"low"},
						},
						"limits": map[string]any{
							"max_context_window_tokens": 200000,
							"max_prompt_tokens":         200000,
							"max_output_tokens":         8000,
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	ctrl := buildTestController(t, func(deps *ControllerDeps) {
		deps.Runtime.SettingsFunc = func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			settings.UpstreamBase = upstream.URL
			return settings, nil
		}
		deps.Runtime.AuthFunc = func() (runtimeconfig.AuthConfig, error) {
			return runtimeconfig.AuthConfig{
				Default:  "alice",
				Accounts: []runtimeconfig.Account{{User: "alice", GhToken: "token-alice"}},
			}, nil
		}
		deps.Runtime.ModelLoader = nil
		deps.Runtime.HTTPClient = upstream.Client()
	})

	got, err := ctrl.ModelService().Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh models: %v", err)
	}
	if len(got) != 1 || got[0].ID != "gpt-5-mini" {
		t.Fatalf("unexpected models: %+v", got)
	}
	if path != runtimeconfig.UpstreamModelsPath {
		t.Fatalf("expected upstream /models path, got %q", path)
	}
	if authHeader != "Bearer token-alice" {
		t.Fatalf("expected Authorization from default account, got %q", authHeader)
	}
}

func TestServiceControllerModelRefreshUsesUpdatedDefaultAccount(t *testing.T) {
	var (
		mu         sync.Mutex
		authHeader string
		authConfig = runtimeconfig.AuthConfig{Default: "alice", Accounts: []runtimeconfig.Account{{User: "alice", GhToken: "token-alice"}, {User: "bob", GhToken: "token-bob"}}}
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeader = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                   "gpt-5-mini",
					"name":                 "GPT-5 Mini",
					"vendor":               "OpenAI",
					"version":              "1",
					"preview":              false,
					"model_picker_enabled": true,
					"supported_endpoints":  []string{runtimeconfig.UpstreamChatCompletionsPath},
					"billing": map[string]any{
						"is_premium": false,
						"multiplier": 1.0,
					},
					"capabilities": map[string]any{
						"family": "gpt-5-mini",
						"type":   "chat",
						"supports": map[string]any{
							"reasoning_effort": []string{"low"},
						},
						"limits": map[string]any{
							"max_context_window_tokens": 200000,
							"max_prompt_tokens":         200000,
							"max_output_tokens":         8000,
						},
					},
				},
			},
		})
	}))
	defer upstream.Close()

	ctrl := buildTestController(t, func(deps *ControllerDeps) {
		deps.Runtime.SettingsFunc = func() (runtimeconfig.RuntimeSettings, error) {
			settings := runtimeconfig.Default()
			settings.ListenAddr = "127.0.0.1:0"
			settings.UpstreamBase = upstream.URL
			return settings, nil
		}
		deps.Runtime.AuthFunc = func() (runtimeconfig.AuthConfig, error) {
			mu.Lock()
			defer mu.Unlock()
			return authConfig, nil
		}
		deps.Runtime.ModelLoader = nil
		deps.Runtime.HTTPClient = upstream.Client()
	})

	if _, err := ctrl.ModelService().Refresh(context.Background()); err != nil {
		t.Fatalf("refresh models with first account: %v", err)
	}
	mu.Lock()
	first := authHeader
	authConfig.Default = "bob"
	mu.Unlock()
	if first != "Bearer token-alice" {
		t.Fatalf("expected first refresh to use alice token, got %q", first)
	}

	if _, err := ctrl.ModelService().Refresh(context.Background()); err != nil {
		t.Fatalf("refresh models with second account: %v", err)
	}
	mu.Lock()
	second := authHeader
	mu.Unlock()
	if second != "Bearer token-bob" {
		t.Fatalf("expected second refresh to use bob token, got %q", second)
	}
}

func startControllerAsync(t *testing.T, ctrl *ServiceController) <-chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Start()
	}()
	waitForCondition(t, func() bool { return ctrl.Status() == core.StateRunning }, "controller start")
	return errCh
}

func stopControllerAndWait(t *testing.T, ctrl *ServiceController, errCh <-chan error) {
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

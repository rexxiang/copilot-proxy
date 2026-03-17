package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/internal/config"
	coreRuntime "copilot-proxy/internal/core/runtime"
	"copilot-proxy/internal/core/runtimeconfig"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

var errRuntimeServerRequired = errors.New("runtime server is required")

// ServerDeps contains injectable dependencies for server construction.
type ServerDeps struct {
	HTTPClient    *http.Client
	Transport     http.RoundTripper
	SettingsFunc  func() (runtimeconfig.Config, error)
	AuthFunc      func() (config.AuthConfig, error)
	Observability middleware.ObservabilitySink
	TokenManager  middleware.TokenProvider
	ModelCatalog  models.MutableCatalog
	ModelLoader   models.Loader
}

type serverRuntime struct {
	runtime *coreRuntime.Runtime
}

const (
	defaultMonitorTimeout = 30 * time.Second
)

func newTimeoutHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// DefaultServerDeps returns production dependencies.
func DefaultServerDeps() ServerDeps {
	return ServerDeps{
		SettingsFunc: loadRuntimeConfigFromAppSettings,
		AuthFunc:     config.LoadAuth,
		ModelCatalog: models.NewManager(),
	}
}

func buildServerWithDeps(deps *ServerDeps) (*serverRuntime, error) {
	return buildServerWithDepsWithContext(context.Background(), deps)
}

func buildServerWithDepsWithContext(ctx context.Context, deps *ServerDeps) (*serverRuntime, error) {
	if deps == nil {
		deps = &ServerDeps{}
	}
	runtimeDeps := coreRuntime.RuntimeDeps{
		HTTPClient:    deps.HTTPClient,
		Transport:     deps.Transport,
		SettingsFunc:  deps.SettingsFunc,
		AuthFunc:      deps.AuthFunc,
		Observability: deps.Observability,
		TokenManager:  deps.TokenManager,
		ModelCatalog:  deps.ModelCatalog,
		ModelLoader:   deps.ModelLoader,
	}
	rt, err := coreRuntime.NewRuntimeWithContext(ctx, runtimeDeps)
	if err != nil {
		return nil, err
	}
	return &serverRuntime{runtime: rt}, nil
}

func runServerWithTUI(enableTUI bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	useTUI := enableTUI && isTTY(os.Stdout.Fd())

	deps := DefaultServerDeps()
	ctrlDeps := ControllerDeps{
		Runtime: coreRuntime.RuntimeDeps{
			HTTPClient:   deps.HTTPClient,
			Transport:    deps.Transport,
			SettingsFunc: deps.SettingsFunc,
			AuthFunc:     deps.AuthFunc,
			TokenManager: deps.TokenManager,
			ModelCatalog: deps.ModelCatalog,
			ModelLoader:  deps.ModelLoader,
		},
	}
	ctrl, err := NewServiceController(ctx, ctrlDeps)
	if err != nil {
		return err
	}
	runtime := ctrl.Runtime()
	if useTUI {
		return runWithTUI(ctx, ctrl)
	}
	if runtime == nil || runtime.Handler == nil {
		return errRuntimeServerRequired
	}
	addr := runtime.ListenAddr
	if addr == "" {
		addr = runtimeconfig.Default().ListenAddr
	}
	defer func() {
		_ = ctrl.Stop()
	}()

	if _, err := fmt.Fprintf(os.Stdout, "Listening on %s\n", addr); err != nil {
		return fmt.Errorf("write listening message: %w", err)
	}

	if err := ctrl.Start(); err != nil && !isExpectedShutdownError(err) {
		return fmt.Errorf("start server: %w", err)
	}
	return nil
}

func isExpectedShutdownError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed)
}

func runWithTUI(ctx context.Context, ctrl *ServiceController) error {
	if ctrl == nil {
		return errRuntimeServerRequired
	}
	runtime := ctrl.Runtime()
	if runtime == nil || runtime.Handler == nil {
		return errRuntimeServerRequired
	}
	addr := runtime.ListenAddr
	if addr == "" {
		addr = runtimeconfig.Default().ListenAddr
	}
	serverErr := make(chan error, 1)
	go func(localCtrl *ServiceController) {
		if localCtrl == nil {
			return
		}
		err := localCtrl.Start()
		if isExpectedShutdownError(err) {
			return
		}
		if err != nil {
			serverErr <- err
		}
	}(ctrl)
	defer func() {
		_ = ctrl.Stop()
	}()

	currentSettings, err := appsettings.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	currentSettings.ListenAddr = addr

	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: currentSettings,
		ValidateRuntime: func(next appsettings.Settings) (RuntimeValidationResult, error) {
			return coreRuntime.CompileSnapshot(appsettings.ToRuntimeConfig(next))
		},
		PersistSettings: func(settings appsettings.Settings) error {
			return appsettings.SaveSettings(&settings)
		},
		PublishRuntime: func(next appsettings.Settings, validated RuntimeValidationResult) error {
			return nil
		},
	})

	modelSvc := ctrl.ModelService()
	accountSvc := newRuntimeAccountManager(
		config.LoadAuth,
		config.SaveAuth,
		loadRuntimeConfigFromAppSettings,
		newTimeoutHTTPClient(defaultMonitorTimeout),
	)
	monitorDeps := MonitorDeps{
		StatsService:   ctrl.StatsService(),
		ModelService:   modelSvc,
		AccountService: accountSvc,
		Models:         nil,
		HTTPClient:     newTimeoutHTTPClient(defaultMonitorTimeout),
		LoadSettings: func() (appsettings.Settings, error) {
			return coordinator.Current(), nil
		},
		ApplySettings: func(settings appsettings.Settings) (appsettings.Settings, error) {
			applied, applyErr := coordinator.Apply(&settings)
			if applyErr != nil {
				return appsettings.Settings{}, applyErr
			}
			return applied, nil
		},
	}
	if modelSvc != nil {
		monitorDeps.Models = modelSvc.List()
	}
	model := NewMonitorModel(&monitorDeps, addr)

	// Run TUI
	program := tea.NewProgram(&model, tea.WithAltScreen())

	// Handle TUI in goroutine to allow server error propagation
	tuiErr := make(chan error, 1)
	go func() {
		_, err := program.Run()
		tuiErr <- err
	}()

	// Wait for either server error, TUI exit, or context cancellation
	select {
	case err := <-serverErr:
		program.Quit()
		return err
	case err := <-tuiErr:
		// TUI exited, signal server to stop
		return err
	case <-ctx.Done():
		program.Quit()
		return nil
	}
}

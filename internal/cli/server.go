package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core/controller"
	coreRuntime "copilot-proxy/internal/core/runtime"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	errModelCatalogRequired     = errors.New("model catalog is required")
	errModelCatalogSetModels    = errors.New("model catalog does not support SetModels")
	errRuntimeServerRequired    = errors.New("runtime server is required")
	errRuntimeAuthStoreRequired = errors.New("runtime auth store is required")
	errRuntimeSettingsRequired  = errors.New("runtime settings store is required")
)

// ServerDeps contains injectable dependencies for server construction.
type ServerDeps struct {
	HTTPClient    *http.Client
	Transport     http.RoundTripper
	SettingsFunc  func() (config.Settings, error)
	AuthFunc      func() (config.AuthConfig, error)
	Observability middleware.ObservabilitySink
	TokenManager  middleware.TokenProvider
	ModelCatalog  models.Catalog
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
		SettingsFunc: config.LoadSettings,
		AuthFunc:     config.LoadAuth,
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

func activateDefaultAccount(
	auth *config.AuthConfig,
	user string,
	save func(config.AuthConfig) error,
) error {
	if auth == nil {
		return config.ErrNoAccountsConfigured
	}
	previousDefault := auth.Default
	if err := auth.SetDefault(user); err != nil {
		return err
	}
	if save == nil {
		return nil
	}
	if err := save(*auth); err != nil {
		auth.Default = previousDefault
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func upsertAccountPreserveDefault(
	auth *config.AuthConfig,
	account config.Account,
	save func(config.AuthConfig) error,
) error {
	if auth == nil {
		return config.ErrNoAccountsConfigured
	}

	previous := cloneAuth(*auth)
	hadAccounts := len(auth.Accounts) > 0
	previousDefault := auth.Default

	auth.UpsertAccount(account)
	if hadAccounts && previousDefault != "" && previousDefault != account.User {
		if err := auth.SetDefault(previousDefault); err != nil {
			*auth = previous
			return err
		}
	}

	if save == nil {
		return nil
	}
	if err := save(*auth); err != nil {
		*auth = previous
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func cloneAuth(auth config.AuthConfig) config.AuthConfig {
	accounts := make([]config.Account, len(auth.Accounts))
	copy(accounts, auth.Accounts)
	auth.Accounts = accounts
	return auth
}

func runServerWithTUI(enableTUI bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	useTUI := enableTUI && isTTY(os.Stdout.Fd())

	deps := DefaultServerDeps()
	ctrlDeps := controller.ControllerDeps{
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
	ctrl, err := controller.NewServiceController(ctx, ctrlDeps)
	if err != nil {
		return err
	}
	runtime := ctrl.Runtime()
	if useTUI {
		return runWithTUI(ctx, ctrl)
	}
	if runtime == nil || runtime.Server == nil {
		return errRuntimeServerRequired
	}
	srv := runtime.Server
	defer func() {
		_ = ctrl.Stop()
	}()

	if _, err := fmt.Fprintf(os.Stdout, "Listening on %s\n", srv.Addr); err != nil {
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

func runWithTUI(ctx context.Context, ctrl *controller.ServiceController) error {
	if ctrl == nil {
		return errRuntimeServerRequired
	}
	runtime := ctrl.Runtime()
	if runtime == nil || runtime.Server == nil {
		return errRuntimeServerRequired
	}
	if runtime.SettingsStore == nil {
		return errRuntimeSettingsRequired
	}
	configSvc := ctrl.ConfigService()
	if configSvc == nil {
		return errors.New("config service is required")
	}
	runtimeMu := sync.Mutex{}
	serverErr := make(chan error, 1)
	runCtx, activeCancel := context.WithCancel(ctx)
	go func(localRuntime *coreRuntime.Runtime, localCtx context.Context) {
		if localRuntime == nil || localRuntime.Server == nil {
			return
		}
		err := localRuntime.Server.Start(localCtx)
		if isExpectedShutdownError(err) || localCtx.Err() != nil {
			return
		}
		serverErr <- err
	}(runtime, runCtx)
	defer func() {
		runtimeMu.Lock()
		defer runtimeMu.Unlock()
		if runtime.RequestCloser != nil {
			_ = runtime.RequestCloser.Close()
		}
		activeCancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer shutdownCancel()
		if runtime.Server != nil {
			_ = runtime.Server.Shutdown(shutdownCtx)
			_ = runtime.Server.Close()
		}
	}()

	currentSettings := runtime.SettingsStore.Current()
	currentSettings.ListenAddr = runtime.Server.Addr

	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: currentSettings,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			return runtime.SettingsStore.Validate(next)
		},
		PersistSettings: func(settings config.Settings) error {
			if _, err := configSvc.Update(settings); err != nil {
				return fmt.Errorf("persist settings: %w", err)
			}
			return nil
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			snapshot, ok := validated.(coreRuntime.Snapshot)
			if !ok {
				return fmt.Errorf("unexpected runtime snapshot type %T", validated)
			}
			runtime.SettingsStore.Publish(next, snapshot)
			return nil
		},
	})

	modelSvc := ctrl.ModelService()
	monitorDeps := MonitorDeps{
		StatsService:   ctrl.StatsService(),
		ModelService:   modelSvc,
		AccountService: ctrl.AccountService(),
		ConfigService:  ctrl.ConfigService(),
		Models:         nil,
		AuthConfig:     &runtime.Auth,
		ActivateAccount: func(user string) error {
			runtimeMu.Lock()
			defer runtimeMu.Unlock()
			if runtime.AuthStore == nil {
				return errRuntimeAuthStoreRequired
			}
			return activateDefaultAccount(&runtime.Auth, user, runtime.AuthStore.SaveAuth)
		},
		AddAccount: func(account config.Account) error {
			runtimeMu.Lock()
			defer runtimeMu.Unlock()
			if runtime.AuthStore == nil {
				return errRuntimeAuthStoreRequired
			}
			return upsertAccountPreserveDefault(&runtime.Auth, account, runtime.AuthStore.SaveAuth)
		},
		HTTPClient: newTimeoutHTTPClient(defaultMonitorTimeout),
		LoadSettings: func() (config.Settings, error) {
			return coordinator.Current(), nil
		},
		ApplySettings: func(settings config.Settings) (config.Settings, error) {
			applied, applyErr := coordinator.Apply(&settings)
			if applyErr != nil {
				return config.Settings{}, applyErr
			}
			return applied, nil
		},
	}
	if modelSvc != nil {
		monitorDeps.Models = modelSvc.List()
	}
	model := NewMonitorModel(&monitorDeps, runtime.Server.Addr)

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

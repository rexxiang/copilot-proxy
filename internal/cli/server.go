package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/monitor"
	"copilot-proxy/internal/proxy"
	"copilot-proxy/internal/server"
	"copilot-proxy/internal/token"

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
	HTTPClient   *http.Client
	Transport    http.RoundTripper
	SettingsFunc func() (config.Settings, error)
	AuthFunc     func() (config.AuthConfig, error)
	Metrics      middleware.MetricsRecorder
	DebugLogger  middleware.DebugLogger
	TokenManager middleware.TokenProvider
	ModelCatalog models.Catalog
	ModelLoader  models.Loader
}

type serverRuntime struct {
	server        *server.Server
	authStore     upstream.AuthStore
	requestCloser interface{ Close() error }
	settingsStore *runtimeSettingsStore
}

const (
	defaultMaxBackoff       = 5 * time.Second
	defaultBackoffFactor    = 2.0
	defaultAutoSaveInterval = 30 * time.Second
	defaultModelTimeout     = 5 * time.Second
	defaultLogRetentionDays = 30
	defaultCollectorMax     = 1000
	defaultMonitorTimeout   = 30 * time.Second
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
	settingsLoader := deps.SettingsFunc
	if settingsLoader == nil {
		settingsLoader = config.LoadSettings
	}
	settings, err := settingsLoader()
	if err != nil {
		return nil, err
	}

	authLoader := deps.AuthFunc
	if authLoader == nil {
		authLoader = config.LoadAuth
	}
	auth, err := authLoader()
	if err != nil {
		auth = config.AuthConfig{Default: "", Accounts: nil}
	}

	var tokens middleware.TokenProvider
	if deps.TokenManager != nil {
		tokens = deps.TokenManager
	} else {
		tokens = token.NewDirectProvider()
	}

	transport := deps.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	settingsStore, err := newRuntimeSettingsStore(settings)
	if err != nil {
		return nil, fmt.Errorf("compile runtime settings snapshot: %w", err)
	}
	transport = proxy.NewDynamicRetryTransport(transport, func() proxy.RetryConfig {
		snapshot := settingsStore.Snapshot()
		return proxy.RetryConfig{
			MaxRetries:     snapshot.MaxRetries,
			InitialBackoff: snapshot.RetryBackoff,
			MaxBackoff:     defaultMaxBackoff,
			BackoffFactor:  defaultBackoffFactor,
		}
	})

	store := newAuthStore(auth)
	requiredHeaders := (&settings).RequiredHeadersWithDefaults()
	modelCatalog := deps.ModelCatalog
	if modelCatalog == nil {
		modelCatalog = models.DefaultModelsManager()
	}
	if err := preloadModels(ctx, &settings, &auth, tokens, deps.HTTPClient, requiredHeaders, modelCatalog, deps.ModelLoader); err != nil {
		return nil, err
	}
	upstreamMiddlewares := []middleware.Middleware{
		upstream.NewStripXHeaders(),
		upstream.NewContextInit(),
		upstream.NewRequestID(),
		upstream.NewResolveAccount(store),
		upstream.NewToken(upstream.TokenConfig{Provider: tokens}),
		upstream.NewParseRequestBodyWithOptionsProvider(func() middleware.ParseOptions {
			snapshot := settingsStore.Snapshot()
			return middleware.ParseOptions{
				MessagesAgentDetectionRequestMode: snapshot.MessagesAgentDetectionRequestMode,
			}
		}),
		upstream.NewCaptureDebug(),
		upstream.NewMessagesTranslateWithRuntimeOptions(modelCatalog, config.PathMapping, func() upstream.MessagesTranslateRuntimeOptions {
			snapshot := settingsStore.Snapshot()
			return upstream.MessagesTranslateRuntimeOptions{
				ClaudeHaikuFallbackModels: snapshot.ClaudeHaikuFallbackModels,
				ReasoningPolicies:         snapshot.ReasoningPolicies,
			}
		}),
		upstream.NewTokenInjection(),
		upstream.NewStaticHeaders(requiredHeaders),
		upstream.NewDynamicHeaders(),
		upstream.NewMetrics(deps.Metrics),
		upstream.NewDebugLog(deps.DebugLogger),
	}
	baseProxyHandler, err := proxy.NewHandler(&proxy.HandlerConfig{
		UpstreamURL:         settings.UpstreamBase,
		Transport:           transport,
		UpstreamMiddlewares: upstreamMiddlewares,
	})
	if err != nil {
		return nil, fmt.Errorf("build proxy handler: %w", err)
	}
	rateLimited := proxy.NewRateLimitedHandlerWithProvider(baseProxyHandler, func() time.Duration {
		snapshot := settingsStore.Snapshot()
		return snapshot.RateLimitCooldown
	})
	var proxyHandler http.Handler = rateLimited
	var requestCloser interface{ Close() error } = rateLimited
	return &serverRuntime{
		server:        server.New(&settings, proxyHandler),
		authStore:     store,
		requestCloser: requestCloser,
		settingsStore: settingsStore,
	}, nil
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

func runServerWithTUI(enableTUI bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Check if TUI should be enabled
	useTUI := enableTUI && isTTY(os.Stdout.Fd())

	monitoring, monitoringErr := initMonitoring()
	if monitoringErr != nil {
		if _, logErr := fmt.Fprintf(os.Stderr, "monitor disabled, fallback to headless: %v\n", monitoringErr); logErr != nil {
			return fmt.Errorf("write monitor warning: %w", logErr)
		}
	}
	var collector *monitor.PersistentCollector
	var debugLogger *monitor.DebugLogger
	var logDir string
	var auth config.AuthConfig
	if monitoringErr == nil {
		collector = monitoring.collector
		debugLogger = monitoring.debugLogger
		logDir = monitoring.logDir
		auth = monitoring.auth
	}

	deps := DefaultServerDeps()
	if monitoringErr == nil {
		deps.Metrics = collector
		deps.DebugLogger = debugLogger
	}
	runtime, err := buildServerWithDeps(&deps)
	if err != nil {
		if monitoringErr == nil {
			_ = debugLogger.Close()
		}
		return err
	}

	if useTUI && monitoringErr == nil {
		return runWithTUI(ctx, runtime, collector, debugLogger, logDir, &auth)
	}
	srv := runtime.server
	// Ensure server resources are released in non-TUI mode.
	defer func() {
		if runtime.requestCloser != nil {
			_ = runtime.requestCloser.Close()
		}
		_ = srv.Close()
	}()

	if monitoringErr == nil {
		defer func() {
			_ = debugLogger.Close()
		}()
	}

	// Headless mode
	if _, err := fmt.Fprintf(os.Stdout, "Listening on %s\n", srv.Addr); err != nil {
		return fmt.Errorf("write listening message: %w", err)
	}

	if monitoringErr == nil {
		// Start auto-save in headless mode too
		stopSave := make(chan struct{})
		collector.StartAutoSave(defaultAutoSaveInterval, stopSave)
		defer func() {
			close(stopSave)
			_ = collector.Save()
		}()
	}

	if err := srv.Start(ctx); err != nil && !isExpectedShutdownError(err) {
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

func preloadModels(
	ctx context.Context,
	settings *config.Settings,
	auth *config.AuthConfig,
	tokens middleware.TokenProvider,
	httpClient *http.Client,
	baseHeaders map[string]string,
	catalog models.Catalog,
	loader models.Loader,
) error {
	if settings == nil || auth == nil {
		return errModelCatalogRequired
	}
	if catalog == nil {
		return errModelCatalogRequired
	}
	target, ok := catalog.(interface{ SetModels([]models.ModelInfo) })
	if !ok {
		return errModelCatalogSetModels
	}
	ctx, cancel := context.WithTimeout(ctx, defaultModelTimeout)
	defer cancel()

	if loader != nil {
		loaded, err := loader.Load(ctx)
		if err != nil {
			return fmt.Errorf("load models: %w", err)
		}
		target.SetModels(loaded)
		return nil
	}

	account, _, err := auth.DefaultAccount()
	if err != nil {
		return fmt.Errorf("resolve default account: %w", err)
	}
	tokenValue, err := tokens.GetToken(ctx, account)
	if err != nil {
		return fmt.Errorf("fetch token: %w", err)
	}
	modelClient := httpClient
	if modelClient == nil {
		modelClient = newTimeoutHTTPClient(defaultModelTimeout)
	}
	loaded, err := models.FetchModels(ctx, modelClient, settings.UpstreamBase, tokenValue, baseHeaders)
	if err != nil {
		return fmt.Errorf("fetch models: %w", err)
	}
	target.SetModels(loaded)
	return nil
}

type monitoringState struct {
	collector   *monitor.PersistentCollector
	debugLogger *monitor.DebugLogger
	logDir      string
	auth        config.AuthConfig
}

func initMonitoring() (*monitoringState, error) {
	auth, err := config.LoadAuth()
	if err != nil {
		auth = config.AuthConfig{Default: "", Accounts: nil}
	}

	dataDir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}
	metricsFile := filepath.Join(dataDir, "metrics.json")
	logDir := filepath.Join(dataDir, "log")

	_ = monitor.CleanOldLogs(logDir, defaultLogRetentionDays)

	collector := monitor.NewPersistentCollector(defaultCollectorMax, metricsFile)
	debugLogger := monitor.NewDebugLogger()
	keepDebugLogger := false
	defer func() {
		if !keepDebugLogger {
			_ = debugLogger.Close()
		}
	}()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := debugLogger.Init(logDir); err != nil {
		return nil, fmt.Errorf("init debug logger: %w", err)
	}

	keepDebugLogger = true

	return &monitoringState{
		collector:   collector,
		debugLogger: debugLogger,
		logDir:      logDir,
		auth:        auth,
	}, nil
}

func runWithTUI(
	ctx context.Context,
	runtime *serverRuntime,
	collector *monitor.PersistentCollector,
	debugLogger *monitor.DebugLogger,
	logDir string,
	auth *config.AuthConfig,
) error {
	if runtime == nil || runtime.server == nil {
		return errRuntimeServerRequired
	}
	if runtime.settingsStore == nil {
		return errRuntimeSettingsRequired
	}
	// Ensure debug logger is closed on exit
	defer func() {
		_ = debugLogger.Close()
	}()

	runtimeMu := sync.Mutex{}
	activeRuntime := runtime
	serverErr := make(chan error, 1)
	runCtx, activeCancel := context.WithCancel(ctx)
	go func(localRuntime *serverRuntime, localCtx context.Context) {
		err := localRuntime.server.Start(localCtx)
		if isExpectedShutdownError(err) || localCtx.Err() != nil {
			return
		}
		serverErr <- err
	}(activeRuntime, runCtx)
	defer func() {
		runtimeMu.Lock()
		defer runtimeMu.Unlock()
		if activeRuntime != nil && activeRuntime.requestCloser != nil {
			_ = activeRuntime.requestCloser.Close()
		}
		activeCancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer shutdownCancel()
		_ = activeRuntime.server.Shutdown(shutdownCtx)
		_ = activeRuntime.server.Close()
	}()

	// Start auto-save
	stopSave := make(chan struct{})
	collector.StartAutoSave(defaultAutoSaveInterval, stopSave)
	defer func() {
		close(stopSave)
		_ = collector.Save()
	}()

	currentSettings := activeRuntime.settingsStore.Current()
	currentSettings.ListenAddr = activeRuntime.server.Addr

	coordinator := NewSettingsRuntimeCoordinator(&RuntimeCoordinatorConfig{
		InitialSettings: currentSettings,
		ValidateRuntime: func(next config.Settings) (RuntimeValidationResult, error) {
			snapshot, err := activeRuntime.settingsStore.Validate(next)
			if err != nil {
				return nil, err
			}
			return snapshot, nil
		},
		PersistSettings: func(settings config.Settings) error {
			if err := config.SaveSettings(&settings); err != nil {
				return fmt.Errorf("save settings: %w", err)
			}
			return nil
		},
		PublishRuntime: func(next config.Settings, validated RuntimeValidationResult) error {
			snapshot, ok := validated.(runtimeSettingsSnapshot)
			if !ok {
				return fmt.Errorf("unexpected runtime snapshot type %T", validated)
			}
			activeRuntime.settingsStore.Publish(next, snapshot)
			return nil
		},
	})

	// Create TUI model with auth config
	// Get cached models if available
	initialModels := models.DefaultModelsManager().GetModels()
	monitorDeps := MonitorDeps{
		Collector:   collector,
		DebugLogger: debugLogger,
		LogDir:      logDir,
		Models:      initialModels,
		AuthConfig:  auth,
		ActivateAccount: func(user string) error {
			runtimeMu.Lock()
			defer runtimeMu.Unlock()
			if activeRuntime == nil || activeRuntime.authStore == nil {
				return errRuntimeAuthStoreRequired
			}
			return activateDefaultAccount(auth, user, activeRuntime.authStore.SaveAuth)
		},
		AddAccount: func(account config.Account) error {
			runtimeMu.Lock()
			defer runtimeMu.Unlock()
			if activeRuntime == nil || activeRuntime.authStore == nil {
				return errRuntimeAuthStoreRequired
			}
			return upsertAccountPreserveDefault(auth, account, activeRuntime.authStore.SaveAuth)
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
	model := NewMonitorModel(&monitorDeps, activeRuntime.server.Addr)

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

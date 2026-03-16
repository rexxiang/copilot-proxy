package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/proxy"
	"copilot-proxy/internal/server"
	"copilot-proxy/internal/token"
)

const (
	defaultMaxBackoff    = 5 * time.Second
	defaultBackoffFactor = 2.0
	defaultModelTimeout  = 5 * time.Second
)

// RuntimeDeps contains injectable dependencies for building the runtime.
type RuntimeDeps struct {
	HTTPClient    *http.Client
	Transport     http.RoundTripper
	SettingsFunc  func() (config.Settings, error)
	AuthFunc      func() (config.AuthConfig, error)
	Observability middleware.ObservabilitySink
	TokenManager  middleware.TokenProvider
	ModelCatalog  models.Catalog
	ModelLoader   models.Loader
}

// Runtime holds the HTTP server runtime resources.
type Runtime struct {
	Server        *server.Server
	AuthStore     upstream.AuthStore
	SettingsStore *SettingsStore
	RequestCloser interface{ Close() error }
	Settings      config.Settings
	Auth          config.AuthConfig
	ModelCatalog  models.Catalog
	ModelLoader   models.Loader
	HTTPClient    *http.Client
	Observability middleware.ObservabilitySink
}

// DefaultRuntimeDeps returns production dependencies.
func DefaultRuntimeDeps() RuntimeDeps {
	return RuntimeDeps{
		SettingsFunc: config.LoadSettings,
		AuthFunc:     config.LoadAuth,
	}
}

// NewRuntime builds the runtime with default context.
func NewRuntime(deps RuntimeDeps) (*Runtime, error) {
	return NewRuntimeWithContext(context.Background(), deps)
}

// NewRuntimeWithContext builds the runtime with the provided context.
func NewRuntimeWithContext(ctx context.Context, deps RuntimeDeps) (*Runtime, error) {
	if deps.SettingsFunc == nil {
		deps.SettingsFunc = config.LoadSettings
	}
	if deps.AuthFunc == nil {
		deps.AuthFunc = config.LoadAuth
	}

	settings, err := deps.SettingsFunc()
	if err != nil {
		return nil, err
	}

	auth, err := deps.AuthFunc()
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

	settingsStore, err := NewSettingsStore(settings)
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

	store := NewAuthStore(auth)
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
		upstream.NewObservabilityMiddleware(deps.Observability),
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

	return &Runtime{
		Server:        server.New(&settings, proxyHandler),
		AuthStore:     store,
		RequestCloser: requestCloser,
		SettingsStore: settingsStore,
		Settings:      settings,
		Auth:          auth,
		ModelCatalog:  modelCatalog,
		ModelLoader:   deps.ModelLoader,
		HTTPClient:    deps.HTTPClient,
		Observability: deps.Observability,
	}, nil
}

func preloadModels(
	ctx context.Context,
	settings *config.Settings,
	auth *config.AuthConfig,
	tokens middleware.TokenProvider,
	client *http.Client,
	baseHeaders map[string]string,
	catalog models.Catalog,
	loader models.Loader,
) error {
	if settings == nil || auth == nil {
		return errors.New("model catalog is required")
	}
	if catalog == nil {
		return errors.New("model catalog is required")
	}
	target, ok := catalog.(interface{ SetModels([]models.ModelInfo) })
	if !ok {
		return errors.New("model catalog does not support SetModels")
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
	modelClient := client
	if modelClient == nil {
		modelClient = &http.Client{Timeout: defaultModelTimeout}
	}
	loaded, err := models.FetchModels(ctx, modelClient, settings.UpstreamBase, tokenValue, baseHeaders)
	if err != nil {
		return fmt.Errorf("fetch models: %w", err)
	}
	target.SetModels(loaded)
	return nil
}

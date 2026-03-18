package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/proxy"
	runtimeapi "copilot-proxy/internal/runtime/api"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
)

const (
	defaultMaxBackoff    = 5 * time.Second
	defaultBackoffFactor = 2.0
	defaultModelTimeout  = 5 * time.Second
)

var errModelCatalogRequired = errors.New("model catalog is required")
var errRuntimeExecutorNotConfigured = errors.New("runtime executor is not configured")
var errRuntimeResolveTokenNotConfigured = errors.New("runtime token resolver is not configured")

// RuntimeDeps contains injectable dependencies for building the runtime.
type RuntimeDeps struct {
	HTTPClient    *http.Client
	Transport     http.RoundTripper
	SettingsFunc  func() (runtimeconfig.RuntimeSettings, error)
	AuthFunc      func() (runtimeconfig.AuthConfig, error)
	Observability middleware.ObservabilitySink
	ModelCatalog  models.MutableCatalog
	ModelLoader   models.Loader
}

// Runtime holds the HTTP server runtime resources.
type Runtime struct {
	ListenAddr    string
	Handler       http.Handler
	RequestCloser interface{ Close() error }
	HTTPClient    *http.Client
	Observability middleware.ObservabilitySink
	ModelCatalog  models.MutableCatalog
	resolveToken  runtimeapi.ResolveTokenFunc
	executor      *runtimeapi.Engine
}

// DefaultRuntimeDeps returns production dependencies.
func DefaultRuntimeDeps() RuntimeDeps {
	return RuntimeDeps{
		SettingsFunc: func() (runtimeconfig.RuntimeSettings, error) {
			return runtimeconfig.Default(), nil
		},
		AuthFunc: runtimeconfig.LoadAuth,
	}
}

// NewRuntime builds the runtime with default context.
func NewRuntime(deps RuntimeDeps) (*Runtime, error) {
	return NewRuntimeWithContext(context.Background(), deps)
}

// NewRuntimeWithContext builds the runtime with the provided context.
func NewRuntimeWithContext(ctx context.Context, deps RuntimeDeps) (*Runtime, error) {
	if deps.SettingsFunc == nil {
		deps.SettingsFunc = func() (runtimeconfig.RuntimeSettings, error) {
			return runtimeconfig.Default(), nil
		}
	}
	if deps.AuthFunc == nil {
		deps.AuthFunc = runtimeconfig.LoadAuth
	}

	settings, err := deps.SettingsFunc()
	if err != nil {
		return nil, err
	}

	auth, err := deps.AuthFunc()
	if err != nil {
		auth = runtimeconfig.AuthConfig{Default: "", Accounts: nil}
	}

	transport := deps.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	if _, err := compileRuntimeSettingsSnapshot(settings); err != nil {
		return nil, fmt.Errorf("compile runtime settings snapshot: %w", err)
	}

	transport = proxy.NewDynamicRetryTransport(transport, func() proxy.RetryConfig {
		snapshot := loadRuntimeSnapshot(deps.SettingsFunc, settings)
		return proxy.RetryConfig{
			MaxRetries:     snapshot.MaxRetries,
			InitialBackoff: snapshot.RetryBackoff,
			MaxBackoff:     defaultMaxBackoff,
			BackoffFactor:  defaultBackoffFactor,
		}
	})

	modelCatalog := deps.ModelCatalog
	if modelCatalog == nil {
		return nil, errModelCatalogRequired
	}
	if err := preloadModels(ctx, &settings, &auth, deps.HTTPClient, settings.RequiredHeadersWithDefaults(), modelCatalog, deps.ModelLoader); err != nil {
		return nil, err
	}

	upstreamMiddlewares := []middleware.Middleware{
		upstream.NewRequestID(),
		upstream.NewObservabilityMiddleware(deps.Observability),
	}

	baseProxyHandler, err := proxy.NewHandler(&proxy.HandlerConfig{
		UpstreamURLProvider: func() string {
			current, err := deps.SettingsFunc()
			if err != nil {
				return settings.UpstreamBase
			}
			return current.UpstreamBase
		},
		Transport:           transport,
		UpstreamMiddlewares: upstreamMiddlewares,
	})
	if err != nil {
		return nil, fmt.Errorf("build proxy handler: %w", err)
	}
	rateLimited := proxy.NewRateLimitedHandlerWithProvider(baseProxyHandler, func() time.Duration {
		snapshot := loadRuntimeSnapshot(deps.SettingsFunc, settings)
		return snapshot.RateLimitCooldown
	})
	var proxyHandler http.Handler = rateLimited
	var requestCloser interface{ Close() error } = rateLimited

	rt := &Runtime{
		ListenAddr:    settings.ListenAddr,
		HTTPClient:    deps.HTTPClient,
		Observability: deps.Observability,
		ModelCatalog:  modelCatalog,
	}
	rt.resolveToken = func(callCtx context.Context, accountRef string) (string, error) {
		return resolveRuntimeToken(callCtx, deps.AuthFunc, accountRef)
	}
	rt.executor = runtimeapi.NewEngine(runtimeapi.Options{
		SettingsProvider: func(context.Context) (runtimeconfig.RuntimeSettings, error) {
			return deps.SettingsFunc()
		},
		HTTPClientFactory: func() *http.Client {
			if deps.HTTPClient != nil {
				return deps.HTTPClient
			}
			return &http.Client{Timeout: 90 * time.Second}
		},
		ResolveToken: rt.resolveToken,
		ResolveModel: func(_ context.Context, modelID string) (runtimeapi.ModelInfo, error) {
			return resolveRuntimeModel(deps.SettingsFunc, modelCatalog, modelID)
		},
		UpstreamDo: func(callCtx context.Context, req *http.Request) (*http.Response, error) {
			return rt.doUpstream(proxyHandler)(callCtx, req)
		},
	})

	rt.Handler = rt.buildExecuteHandler(proxyHandler)
	rt.RequestCloser = requestCloser

	return rt, nil
}

func loadRuntimeSnapshot(settingsFunc func() (runtimeconfig.RuntimeSettings, error), fallback runtimeconfig.RuntimeSettings) Snapshot {
	settings, err := settingsFunc()
	if err != nil {
		settings = fallback
	}
	snapshot, err := compileRuntimeSettingsSnapshot(settings)
	if err != nil {
		snapshot, _ = compileRuntimeSettingsSnapshot(fallback)
	}
	return snapshot
}

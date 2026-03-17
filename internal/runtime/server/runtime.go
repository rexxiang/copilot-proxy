package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream"
	"copilot-proxy/internal/proxy"
	runtimeapi "copilot-proxy/internal/runtime/api"
	runtimeconfig "copilot-proxy/internal/runtime/config"
	execute "copilot-proxy/internal/runtime/execute"
	models "copilot-proxy/internal/runtime/model"
	core "copilot-proxy/internal/runtime/types"
	"copilot-proxy/internal/token"
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

func resolveRuntimeToken(
	ctx context.Context,
	loadAuth func() (runtimeconfig.AuthConfig, error),
	accountRef string,
) (string, error) {
	if loadAuth == nil {
		return "", errors.New("auth loader is required")
	}

	auth, err := loadAuth()
	if err != nil {
		return "", fmt.Errorf("load auth: %w", err)
	}
	account, err := resolveRuntimeAccount(auth, accountRef)
	if err != nil {
		return "", err
	}
	return token.Resolve(ctx, account)
}

func resolveRuntimeAccount(auth runtimeconfig.AuthConfig, accountRef string) (runtimeconfig.Account, error) {
	if trimmed := strings.TrimSpace(accountRef); trimmed != "" {
		for _, account := range auth.Accounts {
			if account.User == trimmed {
				return account, nil
			}
		}
		return runtimeconfig.Account{}, runtimeconfig.ErrAccountNotFound
	}
	account, _, err := auth.DefaultAccount()
	return account, err
}

func resolveRuntimeModel(
	loadSettings func() (runtimeconfig.RuntimeSettings, error),
	catalog models.MutableCatalog,
	modelID string,
) (runtimeapi.ModelInfo, error) {
	resolved := runtimeapi.ModelInfo{ID: strings.TrimSpace(modelID)}
	if strings.TrimSpace(modelID) == "" || catalog == nil {
		return resolved, nil
	}

	selector := models.NewSelector()
	if loadSettings != nil {
		if settings, err := loadSettings(); err == nil {
			selector = models.NewSelectorWithConfig(models.SelectorConfig{
				ClaudeHaikuFallbackModels: settings.ClaudeHaikuFallbackModels,
			})
		}
	}

	selected, _, found := selector.SelectModelInfo(catalog.GetModels(), modelID)
	if !found {
		return resolved, nil
	}
	return runtimeapi.ModelInfo{
		ID:                       selected.ID,
		Endpoints:                append([]string(nil), selected.Endpoints...),
		SupportedReasoningEffort: append([]string(nil), selected.SupportedReasoningEffort...),
	}, nil
}

func preloadModels(
	ctx context.Context,
	settings *runtimeconfig.RuntimeSettings,
	auth *runtimeconfig.AuthConfig,
	client *http.Client,
	baseHeaders map[string]string,
	catalog models.MutableCatalog,
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
		if errors.Is(err, runtimeconfig.ErrNoAccountsConfigured) || errors.Is(err, runtimeconfig.ErrDefaultAccountNotFound) {
			target.SetModels(nil)
			return nil
		}
		return fmt.Errorf("resolve default account: %w", err)
	}
	tokenValue, err := token.Resolve(ctx, account)
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

// RefreshModels reloads model catalog through runtime core capabilities.
func (rt *Runtime) RefreshModels(ctx context.Context, accountRef string) ([]models.ModelInfo, error) {
	if rt == nil || rt.executor == nil {
		return nil, errRuntimeExecutorNotConfigured
	}
	if rt.resolveToken == nil {
		return nil, errRuntimeResolveTokenNotConfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tokenValue, err := rt.resolveToken(ctx, accountRef)
	if err != nil {
		return nil, fmt.Errorf("resolve token: %w", err)
	}
	items, err := rt.executor.FetchModels(ctx, tokenValue)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	if rt.ModelCatalog != nil {
		rt.ModelCatalog.SetModels(items)
	}
	return items, nil
}

func (rt *Runtime) buildExecuteHandler(proxyHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
			return
		}
		if err := r.Body.Close(); err != nil {
			http.Error(w, fmt.Sprintf("close request body: %v", err), http.StatusInternalServerError)
			return
		}

		req := execute.ExecuteRequest{
			Method:     r.Method,
			Path:       r.URL.RequestURI(),
			Headers:    headersFrom(r.Header),
			Body:       body,
			AccountRef: r.Header.Get("X-Copilot-Account"),
			ModelID:    inferModelID(body),
		}

		var wroteResponse bool

		opts := execute.ExecuteOptions{
			Mode:              execute.StreamModeCallback,
			ResultCallback:    rt.buildResultCallback(w, &wroteResponse),
			TelemetryCallback: rt.buildTelemetryCallback(req),
		}

		if rt.executor == nil {
			http.Error(w, "executor is not configured", http.StatusBadGateway)
			return
		}
		if err := rt.executor.Execute(r.Context(), core.RequestInvocation{
			Method: req.Method,
			Path:   req.Path,
			Body:   req.Body,
			Header: req.Headers,
		}, opts); err != nil && !errors.Is(err, context.Canceled) && !wroteResponse {
			if errors.Is(err, runtimeconfig.ErrNoAccountsConfigured) ||
				errors.Is(err, runtimeconfig.ErrDefaultAccountNotFound) ||
				errors.Is(err, runtimeconfig.ErrAccountNotFound) {
				middleware.WriteError(w, http.StatusUnauthorized, "no account available")
				return
			}
			http.Error(w, fmt.Sprintf("execute: %v", err), http.StatusBadGateway)
		}
	})
}

func (rt *Runtime) doUpstream(proxyHandler http.Handler) func(context.Context, *http.Request) (*http.Response, error) {
	invoker := proxy.NewInProcessInvoker(proxyHandler)
	return func(ctx context.Context, upstreamReq *http.Request) (*http.Response, error) {
		if upstreamReq == nil {
			return nil, errors.New("upstream request is required")
		}
		if ctx != nil {
			upstreamReq = upstreamReq.Clone(ctx)
		}
		return invoker.Do(upstreamReq)
	}
}

func (rt *Runtime) buildResultCallback(w http.ResponseWriter, wroteResponse *bool) func(execute.ExecuteResult) {
	flusher, _ := w.(http.Flusher)
	state := struct {
		headerWritten bool
	}{}
	return func(result execute.ExecuteResult) {
		if wroteResponse != nil {
			*wroteResponse = true
		}
		if result.Error != "" {
			if !state.headerWritten {
				w.WriteHeader(http.StatusBadGateway)
				state.headerWritten = true
			}
			if _, err := w.Write([]byte(result.Error)); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		if !state.headerWritten {
			status := result.StatusCode
			if status == 0 {
				status = http.StatusOK
			}
			writeHeaders(w.Header(), result.Headers)
			w.WriteHeader(status)
			state.headerWritten = true
		}

		if len(result.Body) > 0 {
			if _, err := w.Write(result.Body); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (rt *Runtime) buildTelemetryCallback(req execute.ExecuteRequest) func(execute.TelemetryEvent) {
	return func(event execute.TelemetryEvent) {
		if rt.Observability == nil {
			return
		}
		path := event.Path
		if path == "" {
			path = req.Path
		}
		modelID := event.Model
		if modelID == "" {
			modelID = req.ModelID
		}
		payload := map[string]any{
			"path":        path,
			"status_code": event.StatusCode,
			"model":       modelID,
		}
		if event.Error != "" {
			payload["error"] = event.Error
		}
		rt.Observability.AddEvent(core.Event{
			Timestamp: event.Timestamp,
			Type:      "execute." + event.Type,
			Message:   "execute lifecycle event",
			Payload:   payload,
		})
	}
}

func headersFrom(src http.Header) map[string]string {
	headers := make(map[string]string, len(src))
	for k, values := range src {
		if len(values) > 0 {
			headers[k] = values[0]
		}
	}
	return headers
}

func writeHeaders(dst http.Header, values map[string]string) {
	for k, v := range values {
		dst.Set(k, v)
	}
}

func inferModelID(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	value, ok := payload["model"]
	if !ok {
		return ""
	}
	modelID, _ := value.(string)
	return modelID
}

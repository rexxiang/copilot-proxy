package runtimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	coreaccount "copilot-proxy/internal/core/account"
	execute "copilot-proxy/internal/core/execute"
	"copilot-proxy/internal/core/runtimeconfig"
	"copilot-proxy/internal/models"
)

type ResolveTokenFunc func(ctx context.Context, accountRef string) (string, error)

type ResolveModelFunc func(ctx context.Context, modelID string) (ModelInfo, error)

type RequestInvocation = core.RequestInvocation
type ExecuteOptions = execute.ExecuteOptions
type ExecuteResult = execute.ExecuteResult
type StreamMode = execute.StreamMode
type TelemetryEvent = execute.TelemetryEvent
type DeviceCode = auth.DeviceCodeResponse
type UserInfo = core.UserInfo
type CatalogModelInfo = models.ModelInfo

const (
	StreamModeBuffered = execute.StreamModeBuffered
	StreamModeCallback = execute.StreamModeCallback
)

type ModelInfo struct {
	ID                       string
	Endpoints                []string
	SupportedReasoningEffort []string
}

type TelemetryFunc func(ctx context.Context, event TelemetryEvent)

type UpstreamDoFunc func(ctx context.Context, req *http.Request) (*http.Response, error)

type Options struct {
	SettingsProvider  func(ctx context.Context) (runtimeconfig.Config, error)
	HTTPClientFactory func() *http.Client
	ResolveToken      ResolveTokenFunc
	ResolveModel      ResolveModelFunc
	OnTelemetry       TelemetryFunc
	UpstreamDo        UpstreamDoFunc
	GitHubBaseURL     string
}

type Runtime struct {
	settingsProvider  func(ctx context.Context) (runtimeconfig.Config, error)
	httpClientFactory func() *http.Client
	resolveToken      ResolveTokenFunc
	resolveModel      ResolveModelFunc
	onTelemetry       TelemetryFunc
	upstreamDo        UpstreamDoFunc
	githubBaseURL     string
}

func NewRuntime(opts Options) *Runtime {
	settingsProvider := opts.SettingsProvider
	if settingsProvider == nil {
		settingsProvider = func(context.Context) (runtimeconfig.Config, error) {
			return runtimeconfig.Default(), nil
		}
	}
	httpClientFactory := opts.HTTPClientFactory
	if httpClientFactory == nil {
		httpClientFactory = func() *http.Client {
			return &http.Client{Timeout: 90 * time.Second}
		}
	}
	githubBaseURL := strings.TrimSpace(opts.GitHubBaseURL)
	if githubBaseURL == "" {
		githubBaseURL = config.GitHubAPIURL
	}
	return &Runtime{
		settingsProvider:  settingsProvider,
		httpClientFactory: httpClientFactory,
		resolveToken:      opts.ResolveToken,
		resolveModel:      opts.ResolveModel,
		onTelemetry:       opts.OnTelemetry,
		upstreamDo:        opts.UpstreamDo,
		githubBaseURL:     githubBaseURL,
	}
}

func (r *Runtime) Execute(ctx context.Context, invocation RequestInvocation, opts ExecuteOptions) error {
	if strings.TrimSpace(invocation.Method) == "" {
		return errors.New("request method is required")
	}
	if strings.TrimSpace(invocation.Path) == "" {
		return errors.New("request path is required")
	}

	settings, err := r.settingsProvider(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	modelID := inferModelID(invocation.Body)

	resolvedModel := ModelInfo{ID: modelID}
	if modelID != "" && r.resolveModel != nil {
		model, resolveErr := r.resolveModel(ctx, modelID)
		if resolveErr != nil {
			return fmt.Errorf("resolve model: %w", resolveErr)
		}
		resolvedModel = model
		if strings.TrimSpace(resolvedModel.ID) == "" {
			resolvedModel.ID = modelID
		}
	}

	requestPath := invocation.Path

	execReq := execute.ExecuteRequest{
		Method:     invocation.Method,
		Path:       requestPath,
		Headers:    cloneHeaders(invocation.Header),
		Body:       cloneBytes(invocation.Body),
		AccountRef: accountReference(invocation.Header),
		ModelID:    strings.TrimSpace(resolvedModel.ID),
	}

	deps := execute.ExecuteDeps{
		DoUpstream: func(callCtx context.Context, req *http.Request) (*http.Response, error) {
			return r.doExecuteUpstream(callCtx, req, settings, modelID, execReq.AccountRef, resolvedModel)
		},
		ResolveToken: r.resolveToken,
		ResolveModel: func(callCtx context.Context, id string) (execute.ModelCapabilities, error) {
			if r.resolveModel == nil {
				return execute.ModelCapabilities{}, nil
			}
			model, resolveErr := r.resolveModel(callCtx, id)
			if resolveErr != nil {
				return execute.ModelCapabilities{}, resolveErr
			}
			return execute.ModelCapabilities{
				ID:        model.ID,
				Endpoints: append([]string(nil), model.Endpoints...),
			}, nil
		},
	}

	telemetryCallback := opts.TelemetryCallback
	if r.onTelemetry != nil {
		upstreamTelemetry := telemetryCallback
		telemetryCallback = func(event TelemetryEvent) {
			r.onTelemetry(ctx, event)
			if upstreamTelemetry != nil {
				upstreamTelemetry(event)
			}
		}
	}

	execOpts := ExecuteOptions{
		Mode:              opts.Mode,
		ResultCallback:    opts.ResultCallback,
		TelemetryCallback: telemetryCallback,
	}

	return execute.Execute(ctx, execReq, deps, execOpts)
}

func (r *Runtime) RequestCode(ctx context.Context) (auth.DeviceCodeResponse, error) {
	flow := auth.DeviceFlow{
		ClientID: config.OAuthClientID,
		Scope:    config.OAuthScope,
		BaseURL:  r.githubBaseURL,
	}
	return flow.RequestCodeWithContext(ctx)
}

func (r *Runtime) PollToken(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
	flow := auth.DeviceFlow{
		ClientID: config.OAuthClientID,
		Scope:    config.OAuthScope,
		BaseURL:  r.githubBaseURL,
	}
	return flow.PollAccessTokenWithContext(ctx, device)
}

func (r *Runtime) FetchUserInfo(ctx context.Context, tokenValue string) (*core.UserInfo, error) {
	client := r.httpClientFactory()
	return coreaccount.FetchUserInfo(ctx, client, r.githubBaseURL, tokenValue)
}

func (r *Runtime) FetchLogin(ctx context.Context, tokenValue string) (string, error) {
	client := r.httpClientFactory()
	return auth.FetchUserWithContext(ctx, client, r.githubBaseURL, tokenValue)
}

func (r *Runtime) FetchModels(ctx context.Context, tokenValue string) ([]models.ModelInfo, error) {
	settings, err := r.settingsProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	modelsAPIBase := strings.TrimSpace(settings.UpstreamBase)
	if modelsAPIBase == "" {
		modelsAPIBase = runtimeconfig.Default().UpstreamBase
	}
	client := r.httpClientFactory()
	return models.FetchModels(ctx, client, modelsAPIBase, tokenValue, settings.RequiredHeadersWithDefaults())
}

func (r *Runtime) doUpstreamRequest(ctx context.Context, upstreamReq *http.Request, settings runtimeconfig.Config) (*http.Response, error) {
	if upstreamReq == nil {
		return nil, errors.New("upstream request is required")
	}

	base := strings.TrimSuffix(settings.UpstreamBase, "/")
	if base == "" {
		base = strings.TrimSuffix(runtimeconfig.Default().UpstreamBase, "/")
	}
	targetPath := upstreamReq.URL.Path
	if targetPath == "" {
		targetPath = upstreamReq.URL.RequestURI()
	}
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	targetURL := base + targetPath
	if upstreamReq.URL != nil && upstreamReq.URL.RawQuery != "" {
		targetURL += "?" + upstreamReq.URL.RawQuery
	}

	request, err := http.NewRequestWithContext(ctx, upstreamReq.Method, targetURL, upstreamReq.Body)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	for key, values := range upstreamReq.Header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	for key, value := range settings.RequiredHeadersWithDefaults() {
		if request.Header.Get(key) == "" {
			request.Header.Set(key, value)
		}
	}
	client := r.httpClientFactory()
	return client.Do(request)
}

func accountReference(header map[string]string) string {
	for key, value := range header {
		lower := strings.ToLower(key)
		if lower == "x-copilot-account" || lower == "x-account" {
			return value
		}
	}
	return ""
}

func inferModelID(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(buf, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"model", "model_id"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		if modelID, ok := value.(string); ok {
			return modelID
		}
	}
	return ""
}

func cloneHeaders(header map[string]string) map[string]string {
	if len(header) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(header))
	for key, value := range header {
		cloned[key] = value
	}
	return cloned
}

func cloneBytes(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	cloned := make([]byte, len(data))
	copy(cloned, data)
	return cloned
}

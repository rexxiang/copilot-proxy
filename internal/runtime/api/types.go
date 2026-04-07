package api

import (
	"context"
	"net/http"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	execute "copilot-proxy/internal/runtime/execute"
	auth "copilot-proxy/internal/runtime/identity/oauth"
	models "copilot-proxy/internal/runtime/model"
	core "copilot-proxy/internal/runtime/types"
)

type ResolveTokenFunc func(ctx context.Context, accountRef string) (string, error)

type ResolveModelFunc func(ctx context.Context, modelID string) (ModelInfo, error)

type StateSetNewFunc func(ctx context.Context, namespace, key, value string) (created bool, err error)

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
	SettingsProvider  func(ctx context.Context) (runtimeconfig.RuntimeSettings, error)
	HTTPClientFactory func() *http.Client
	ResolveToken      ResolveTokenFunc
	ResolveModel      ResolveModelFunc
	StateSetNew       StateSetNewFunc
	OnTelemetry       TelemetryFunc
	UpstreamDo        UpstreamDoFunc
	// GitHubBaseURL is a legacy fallback applied to both OAuth and API calls when
	// GitHubOAuthBaseURL / GitHubAPIBaseURL are unset.
	GitHubBaseURL      string
	GitHubOAuthBaseURL string
	GitHubAPIBaseURL   string
}

type Engine struct {
	settingsProvider   func(ctx context.Context) (runtimeconfig.RuntimeSettings, error)
	httpClientFactory  func() *http.Client
	resolveToken       ResolveTokenFunc
	resolveModel       ResolveModelFunc
	stateSetNew        StateSetNewFunc
	onTelemetry        TelemetryFunc
	upstreamDo         UpstreamDoFunc
	githubOAuthBaseURL string
	githubAPIBaseURL   string
}

package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

func NewEngine(opts Options) *Engine {
	settingsProvider := opts.SettingsProvider
	if settingsProvider == nil {
		settingsProvider = func(context.Context) (runtimeconfig.RuntimeSettings, error) {
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
		githubBaseURL = runtimeconfig.GitHubAPIURL
	}
	return &Engine{
		settingsProvider:  settingsProvider,
		httpClientFactory: httpClientFactory,
		resolveToken:      opts.ResolveToken,
		resolveModel:      opts.ResolveModel,
		onTelemetry:       opts.OnTelemetry,
		upstreamDo:        opts.UpstreamDo,
		githubBaseURL:     githubBaseURL,
	}
}

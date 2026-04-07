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
	legacyBaseURL := strings.TrimSpace(opts.GitHubBaseURL)
	githubOAuthBaseURL := strings.TrimSpace(opts.GitHubOAuthBaseURL)
	if githubOAuthBaseURL == "" {
		if legacyBaseURL != "" {
			githubOAuthBaseURL = legacyBaseURL
		} else {
			githubOAuthBaseURL = runtimeconfig.GitHubBaseURL
		}
	}
	githubAPIBaseURL := strings.TrimSpace(opts.GitHubAPIBaseURL)
	if githubAPIBaseURL == "" {
		if legacyBaseURL != "" {
			githubAPIBaseURL = legacyBaseURL
		} else {
			githubAPIBaseURL = runtimeconfig.GitHubAPIURL
		}
	}
	return &Engine{
		settingsProvider:   settingsProvider,
		httpClientFactory:  httpClientFactory,
		resolveToken:       opts.ResolveToken,
		resolveModel:       opts.ResolveModel,
		stateSetNew:        opts.StateSetNew,
		onTelemetry:        opts.OnTelemetry,
		upstreamDo:         opts.UpstreamDo,
		githubOAuthBaseURL: githubOAuthBaseURL,
		githubAPIBaseURL:   githubAPIBaseURL,
	}
}

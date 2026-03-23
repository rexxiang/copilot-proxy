package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
)

func (r *Engine) FetchModels(ctx context.Context, tokenValue string) ([]models.ModelInfo, error) {
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

func (r *Engine) doUpstreamRequest(ctx context.Context, upstreamReq *http.Request, settings runtimeconfig.RuntimeSettings) (*http.Response, error) {
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

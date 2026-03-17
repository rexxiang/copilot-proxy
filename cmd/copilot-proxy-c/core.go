package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core/runtimeapi"
)

type resolveToken func(ctx context.Context, accountRef string) (string, error)
type resolveModel func(ctx context.Context, modelID string) (modelInfo, error)
type resultCallback func(status int, headers map[string]string, body []byte, errMsg string)
type telemetryCallback func(event map[string]any)

type executeDeps struct {
	ResolveToken resolveToken
	ResolveModel resolveModel
}

type executeOptions struct {
	ResultCallback    resultCallback
	TelemetryCallback telemetryCallback
}

type modelInfo struct {
	ID                       string   `json:"id"`
	Endpoints                []string `json:"endpoints"`
	SupportedReasoningEffort []string `json:"supported_reasoning_effort,omitempty"`
}

var (
	githubAPIBase    = config.GitHubAPIURL
	httpClientMaker  = func() *http.Client { return &http.Client{Timeout: 90 * time.Second} }
	settingsProvider = func() config.Settings {
		return config.DefaultSettings()
	}
)

func executeRequest(ctx context.Context, requestJSON string, deps executeDeps, opts executeOptions) error {
	if opts.ResultCallback == nil {
		return errors.New("result callback is required")
	}

	var req runtimeapi.RequestInvocation
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}
	runtime := newRuntime(deps.ResolveToken, deps.ResolveModel)

	execOpts := runtimeapi.ExecuteOptions{
		Mode: runtimeapi.StreamModeCallback,
		ResultCallback: func(result runtimeapi.ExecuteResult) {
			opts.ResultCallback(result.StatusCode, result.Headers, result.Body, result.Error)
		},
		TelemetryCallback: func(event runtimeapi.TelemetryEvent) {
			emitTelemetry(opts.TelemetryCallback, telemetryEventMap(event))
		},
	}

	return runtime.Execute(ctx, req, execOpts)
}

func newRuntime(resolveTokenFn resolveToken, resolveModelFn resolveModel) *runtimeapi.Runtime {
	opts := runtimeapi.Options{
		SettingsProvider: func(context.Context) (config.Settings, error) {
			return settingsProvider(), nil
		},
		HTTPClientFactory: httpClientMaker,
		GitHubBaseURL:     githubAPIBase,
	}
	if resolveTokenFn != nil {
		opts.ResolveToken = func(ctx context.Context, accountRef string) (string, error) {
			return resolveTokenFn(ctx, accountRef)
		}
	}
	if resolveModelFn != nil {
		opts.ResolveModel = func(ctx context.Context, modelID string) (runtimeapi.ModelInfo, error) {
			info, err := resolveModelFn(ctx, modelID)
			if err != nil {
				return runtimeapi.ModelInfo{}, err
			}
			return runtimeapi.ModelInfo{
				ID:                       info.ID,
				Endpoints:                append([]string(nil), info.Endpoints...),
				SupportedReasoningEffort: append([]string(nil), info.SupportedReasoningEffort...),
			}, nil
		}
	}
	return runtimeapi.NewRuntime(opts)
}

func telemetryEventMap(event runtimeapi.TelemetryEvent) map[string]any {
	payload := map[string]any{
		"type":      event.Type,
		"timestamp": event.Timestamp.Format(time.RFC3339Nano),
	}
	if event.Path != "" {
		payload["path"] = event.Path
	}
	if event.Model != "" {
		payload["model"] = event.Model
	}
	if event.StatusCode != 0 {
		payload["status_code"] = event.StatusCode
	}
	if event.Error != "" {
		payload["error"] = event.Error
	}
	return payload
}

func emitTelemetry(cb telemetryCallback, event map[string]any) {
	if cb == nil {
		return
	}
	cb(event)
}

func requestCodeJSON(ctx context.Context) (string, error) {
	runtime := newRuntime(nil, nil)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	device, err := runtime.RequestCode(ctx)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(device)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func pollTokenJSON(ctx context.Context, payload string) (string, error) {
	var device auth.DeviceCodeResponse
	if err := json.Unmarshal([]byte(payload), &device); err != nil {
		return "", err
	}
	runtime := newRuntime(nil, nil)
	timeout := time.Duration(device.ExpiresIn+30) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tokenValue, err := runtime.PollToken(ctx, device)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]string{"token": tokenValue})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fetchUserInfoJSON(ctx context.Context, tokenValue string) (string, error) {
	runtime := newRuntime(nil, nil)
	info, err := runtime.FetchUserInfo(ctx, tokenValue)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func fetchModelsJSON(ctx context.Context, tokenValue string) (string, error) {
	runtime := newRuntime(nil, nil)
	data, err := runtime.FetchModels(ctx, tokenValue)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	runtimeapi "copilot-proxy/internal/runtime/api"
	config "copilot-proxy/internal/runtime/config"
	auth "copilot-proxy/internal/runtime/identity/oauth"
)

type resolveToken func(ctx context.Context, accountRef string) (string, error)
type resolveModel func(ctx context.Context, modelID string) (modelInfo, error)
type stateSetNew func(ctx context.Context, namespace, key, value string) (bool, error)
type hostDispatch func(ctx context.Context, request hostDispatchRequest) (hostDispatchResponse, error)
type eventCallback func(event eventEnvelope)

type executeDeps struct {
	ResolveToken resolveToken
	ResolveModel resolveModel
	StateSetNew  stateSetNew
}

type executeOptions struct {
	EventCallback eventCallback
}

type hostBridge struct {
	Version      uint32
	Capabilities uint64
	Dispatch     hostDispatch
}

type hostDispatchRequest struct {
	Version int    `json:"version"`
	Op      string `json:"op"`
	Payload any    `json:"payload,omitempty"`
}

type hostDispatchResponse struct {
	Version int             `json:"version"`
	OK      bool            `json:"ok"`
	Code    string          `json:"code,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type eventEnvelope struct {
	Version int            `json:"version"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`
}

type modelInfo struct {
	ID                       string   `json:"id"`
	Endpoints                []string `json:"endpoints"`
	SupportedReasoningEffort []string `json:"supported_reasoning_effort,omitempty"`
}

var (
	githubOAuthBase  = config.GitHubBaseURL
	githubAPIBase    = config.GitHubAPIURL
	httpClientMaker  = func() *http.Client { return &http.Client{Timeout: 90 * time.Second} }
	settingsProvider = func() config.RuntimeSettings {
		return config.Default()
	}
)

const (
	hostDispatchVersion  = 1
	eventEnvelopeVersion = 1

	hostOpResolveToken = "auth.resolve_token"
	hostOpResolveModel = "model.resolve"
	hostOpStateSetNew  = "state.set_new"

	sessionNamespace = "claude_session_seen"
	sessionValue     = "1"
)

type tokenResolveRequest struct {
	AccountRef string `json:"account_ref"`
}

type tokenResolveResponse struct {
	Token string `json:"token"`
}

type modelResolveRequest struct {
	ModelID string `json:"model_id"`
}

type stateSetNewRequest struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

type stateSetNewResponse struct {
	Created bool `json:"created"`
}

func executeRequest(ctx context.Context, requestJSON string, deps executeDeps, opts executeOptions) error {
	if opts.EventCallback == nil {
		return errors.New("event callback is required")
	}

	var req runtimeapi.RequestInvocation
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}
	runtime := newRuntime(deps)

	execOpts := runtimeapi.ExecuteOptions{
		Mode: runtimeapi.StreamModeCallback,
		ResultCallback: func(result runtimeapi.ExecuteResult) {
			emitExecuteResult(opts.EventCallback, result)
		},
		TelemetryCallback: func(event runtimeapi.TelemetryEvent) {
			emitEvent(opts.EventCallback, "telemetry", telemetryEventMap(event))
		},
	}

	return runtime.Execute(ctx, req, execOpts)
}

func newRuntime(deps executeDeps) *runtimeapi.Engine {
	opts := runtimeapi.Options{
		SettingsProvider: func(context.Context) (config.RuntimeSettings, error) {
			return settingsProvider(), nil
		},
		HTTPClientFactory:  httpClientMaker,
		GitHubOAuthBaseURL: githubOAuthBase,
		GitHubAPIBaseURL:   githubAPIBase,
	}
	if deps.ResolveToken != nil {
		opts.ResolveToken = func(ctx context.Context, accountRef string) (string, error) {
			return deps.ResolveToken(ctx, accountRef)
		}
	}
	if deps.ResolveModel != nil {
		opts.ResolveModel = func(ctx context.Context, modelID string) (runtimeapi.ModelInfo, error) {
			info, err := deps.ResolveModel(ctx, modelID)
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
	if deps.StateSetNew != nil {
		opts.StateSetNew = func(ctx context.Context, namespace, key, value string) (bool, error) {
			return deps.StateSetNew(ctx, namespace, key, value)
		}
	}
	return runtimeapi.NewEngine(opts)
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

func emitEvent(cb eventCallback, kind string, payload map[string]any) {
	if cb == nil {
		return
	}
	cb(eventEnvelope{
		Version: eventEnvelopeVersion,
		Kind:    kind,
		Payload: payload,
	})
}

func emitExecuteResult(cb eventCallback, result runtimeapi.ExecuteResult) {
	if cb == nil {
		return
	}
	if result.Error != "" {
		emitEvent(cb, "fatal", map[string]any{
			"error": result.Error,
		})
		return
	}

	if result.StatusCode != 0 {
		payload := map[string]any{
			"status_code": result.StatusCode,
		}
		if len(result.Headers) > 0 {
			payload["headers"] = result.Headers
		}
		if len(result.Body) > 0 {
			payload["body_base64"] = base64.StdEncoding.EncodeToString(result.Body)
		}
		emitEvent(cb, "response_head", payload)
		return
	}

	if len(result.Body) > 0 {
		emitEvent(cb, "response_chunk", map[string]any{
			"body_base64": base64.StdEncoding.EncodeToString(result.Body),
		})
	}
}

func buildExecuteDeps(bridge hostBridge) executeDeps {
	if bridge.Dispatch == nil {
		return executeDeps{}
	}
	return executeDeps{
		ResolveToken: func(ctx context.Context, accountRef string) (string, error) {
			if strings.TrimSpace(accountRef) == "" {
				return "", nil
			}
			var response tokenResolveResponse
			if err := invokeHostOperation(ctx, bridge.Dispatch, hostOpResolveToken, tokenResolveRequest{
				AccountRef: accountRef,
			}, &response); err != nil {
				return "", err
			}
			return strings.TrimSpace(response.Token), nil
		},
		ResolveModel: func(ctx context.Context, modelID string) (modelInfo, error) {
			var response modelInfo
			if err := invokeHostOperation(ctx, bridge.Dispatch, hostOpResolveModel, modelResolveRequest{
				ModelID: modelID,
			}, &response); err != nil {
				return modelInfo{}, err
			}
			return response, nil
		},
		StateSetNew: func(ctx context.Context, namespace, key, value string) (bool, error) {
			var response stateSetNewResponse
			if err := invokeHostOperation(ctx, bridge.Dispatch, hostOpStateSetNew, stateSetNewRequest{
				Namespace: namespace,
				Key:       key,
				Value:     value,
			}, &response); err != nil {
				return false, err
			}
			return response.Created, nil
		},
	}
}

func invokeHostOperation(ctx context.Context, dispatch hostDispatch, op string, payload any, out any) error {
	if dispatch == nil {
		return errors.New("host dispatch is required")
	}

	resp, err := dispatch(ctx, hostDispatchRequest{
		Version: hostDispatchVersion,
		Op:      op,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if !resp.OK {
		errMsg := strings.TrimSpace(resp.Error)
		if errMsg == "" {
			errMsg = strings.TrimSpace(resp.Code)
		}
		if errMsg == "" {
			errMsg = "host dispatch returned non-ok response"
		}
		return fmt.Errorf("%s: %s", op, errMsg)
	}

	if out == nil {
		return nil
	}
	if len(resp.Payload) == 0 {
		return fmt.Errorf("%s: empty response payload", op)
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		return fmt.Errorf("%s: decode response payload: %w", op, err)
	}
	return nil
}

func requestCodeJSON(ctx context.Context) (string, error) {
	runtime := newRuntime(executeDeps{})
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
	runtime := newRuntime(executeDeps{})
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
	runtime := newRuntime(executeDeps{})
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
	runtime := newRuntime(executeDeps{})
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

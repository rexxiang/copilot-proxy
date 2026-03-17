package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	execute "copilot-proxy/internal/runtime/execute"
)

func (r *Engine) Execute(ctx context.Context, invocation RequestInvocation, opts ExecuteOptions) error {
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

	execReq := execute.ExecuteRequest{
		Method:     invocation.Method,
		Path:       invocation.Path,
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

package middleware

import (
	"net/http"

	transportmiddleware "copilot-proxy/internal/middleware"
	"copilot-proxy/internal/reasoning"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
	models "copilot-proxy/internal/runtime/model"
)

// MessagesTranslateMiddleware atomically handles endpoint-related request processing.
type MessagesTranslateMiddleware struct {
	catalog                models.Catalog
	pathMapping            map[string]string
	reasoningPolicies      []reasoning.Policy
	runtimeOptionsProvider func() MessagesTranslateRuntimeOptions
}

// MessagesTranslateRuntimeOptions controls endpoint routing and translation behavior per request.
type MessagesTranslateRuntimeOptions struct {
	ClaudeHaikuFallbackModels []string
	ReasoningPolicies         []reasoning.Policy
}

// NewMessagesTranslateWithRuntimeOptions builds endpoint middleware with per-request runtime options.
func NewMessagesTranslateWithRuntimeOptions(
	catalog models.Catalog,
	mapping map[string]string,
	provider func() MessagesTranslateRuntimeOptions,
) *MessagesTranslateMiddleware {
	parsedPolicies, _ := reasoning.EffectivePoliciesFromMap(nil)
	return &MessagesTranslateMiddleware{
		catalog:                catalog,
		pathMapping:            mapping,
		reasoningPolicies:      parsedPolicies,
		runtimeOptionsProvider: provider,
	}
}

func (m *MessagesTranslateMiddleware) Handle(ctx *transportmiddleware.Context, next transportmiddleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	req := ctx.Request
	rc := endpointflow.EnsureRequestContext(req)
	runtimeOptions := endpointflow.RuntimeOptions{
		ReasoningPolicies: endpointflow.CloneReasoningPolicies(m.reasoningPolicies),
	}
	if m.runtimeOptionsProvider != nil {
		provided := m.runtimeOptionsProvider()
		if provided.ClaudeHaikuFallbackModels != nil {
			runtimeOptions.ClaudeHaikuFallbackModels = provided.ClaudeHaikuFallbackModels
		}
		if provided.ReasoningPolicies != nil {
			runtimeOptions.ReasoningPolicies = endpointflow.CloneReasoningPolicies(provided.ReasoningPolicies)
		}
	}

	resp, err := endpointflow.ApplyCatalogEndpointTransform(req, rc, m.catalog, m.pathMapping, runtimeOptions, func(nextReq *http.Request, nextRC *transportmiddleware.RequestContext) (*http.Response, error) {
		ctx.Request = endpointflow.WithRequestContext(nextReq, nextRC)
		return next()
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

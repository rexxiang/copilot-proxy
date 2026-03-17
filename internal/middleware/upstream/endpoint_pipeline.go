package upstream

import (
	"net/http"

	endpointflow "copilot-proxy/internal/core/endpoint/flow"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/reasoning"
)

// MessagesTranslateMiddleware atomically handles endpoint-related request processing.
type MessagesTranslateMiddleware struct {
	catalog                models.Catalog
	pathMapping            map[string]string
	reasoningPolicies      []reasoning.Policy
	runtimeOptionsProvider func() MessagesTranslateRuntimeOptions
}

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

func (m *MessagesTranslateMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	req := ctx.Request
	rc := ensureRequestContext(req)
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
	resp, err := endpointflow.ApplyCatalogEndpointTransform(req, rc, m.catalog, m.pathMapping, runtimeOptions, func(req *http.Request, rc *middleware.RequestContext) (*http.Response, error) {
		ctx.Request = withRequestContext(req, rc)
		return next()
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func cloneReasoningPolicies(items []reasoning.Policy) []reasoning.Policy {
	return endpointflow.CloneReasoningPolicies(items)
}

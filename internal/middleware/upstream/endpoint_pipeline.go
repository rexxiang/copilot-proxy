package upstream

import (
	"copilot-proxy/internal/runtime/reasoning"
	"net/http"

	"copilot-proxy/internal/middleware"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
	models "copilot-proxy/internal/runtime/model"
	runtimemiddleware "copilot-proxy/internal/runtime/server/middleware"
)

// MessagesTranslateMiddleware remains as a compatibility shim while endpoint middleware
// implementation has moved to internal/runtime/server/middleware.
type MessagesTranslateMiddleware struct {
	catalog                models.Catalog
	pathMapping            map[string]string
	reasoningPolicies      []reasoning.Policy
	runtimeOptionsProvider func() MessagesTranslateRuntimeOptions
}

// MessagesTranslateRuntimeOptions controls endpoint routing and translation behavior per request.
type MessagesTranslateRuntimeOptions = runtimemiddleware.MessagesTranslateRuntimeOptions

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
	if m == nil {
		return next()
	}

	delegate := runtimemiddleware.NewMessagesTranslateWithRuntimeOptions(m.catalog, m.pathMapping, func() runtimemiddleware.MessagesTranslateRuntimeOptions {
		opts := runtimemiddleware.MessagesTranslateRuntimeOptions{
			ReasoningPolicies: cloneReasoningPolicies(m.reasoningPolicies),
		}
		if m.runtimeOptionsProvider != nil {
			provided := m.runtimeOptionsProvider()
			if provided.ClaudeHaikuFallbackModels != nil {
				opts.ClaudeHaikuFallbackModels = provided.ClaudeHaikuFallbackModels
			}
			if provided.ReasoningPolicies != nil {
				opts.ReasoningPolicies = cloneReasoningPolicies(provided.ReasoningPolicies)
			}
		}
		return opts
	})
	return delegate.Handle(ctx, next)
}

func cloneReasoningPolicies(items []reasoning.Policy) []reasoning.Policy {
	return endpointflow.CloneReasoningPolicies(items)
}

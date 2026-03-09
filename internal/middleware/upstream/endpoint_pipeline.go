package upstream

import (
	"fmt"
	"net/http"
	"strings"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/middleware/upstream/transform"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/reasoning"
)

// MessagesTranslateMiddleware atomically handles endpoint-related request processing.
type MessagesTranslateMiddleware struct {
	catalog                models.Catalog
	selector               *models.Selector
	pathMapping            map[string]string
	reasoningPolicies      []reasoning.Policy
	runtimeOptionsProvider func() MessagesTranslateRuntimeOptions
	codec                  transform.EndpointCodec
}

type MessagesTranslateRuntimeOptions struct {
	ClaudeHaikuFallbackModels []string
	ReasoningPolicies         []reasoning.Policy
}

// NewMessagesTranslate builds a single middleware for:
// model rewrite -> endpoint select -> endpoint transform -> path rewrite.
func NewMessagesTranslate(
	catalog models.Catalog,
	selector *models.Selector,
	mapping map[string]string,
	reasoningPolicyMaps ...map[string]string,
) *MessagesTranslateMiddleware {
	if selector == nil {
		selector = models.NewSelector()
	}

	parsedPolicies, _ := reasoning.EffectivePoliciesFromMap(nil)
	if len(reasoningPolicyMaps) > 0 {
		if policies, err := reasoning.EffectivePoliciesFromMap(reasoningPolicyMaps[0]); err == nil {
			parsedPolicies = policies
		}
	}

	return &MessagesTranslateMiddleware{
		catalog:           catalog,
		selector:          selector,
		pathMapping:       mapping,
		reasoningPolicies: parsedPolicies,
		codec: transform.EndpointCodec{
			MessagesToChatRequest:       transform.MessagesToChatRequest,
			ChatToMessagesResponse:      transform.ChatToMessagesResponse,
			ChatSSEToMessages:           transform.TranslateChatSSEToMessages,
			MessagesToResponsesRequest:  transform.MessagesToResponsesRequest,
			ResponsesToMessagesResponse: transform.ResponsesToMessagesResponse,
			ResponsesSSEToMessages:      transform.TranslateResponsesSSEToMessages,
		},
	}
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
		selector:               models.NewSelector(),
		pathMapping:            mapping,
		reasoningPolicies:      parsedPolicies,
		runtimeOptionsProvider: provider,
		codec: transform.EndpointCodec{
			MessagesToChatRequest:       transform.MessagesToChatRequest,
			ChatToMessagesResponse:      transform.ChatToMessagesResponse,
			ChatSSEToMessages:           transform.TranslateChatSSEToMessages,
			MessagesToResponsesRequest:  transform.MessagesToResponsesRequest,
			ResponsesToMessagesResponse: transform.ResponsesToMessagesResponse,
			ResponsesSSEToMessages:      transform.TranslateResponsesSSEToMessages,
		},
	}
}

func (m *MessagesTranslateMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	req := ctx.Request
	rc := ensureRequestContext(req)
	selector := m.selector
	reasoningPolicies := cloneReasoningPolicies(m.reasoningPolicies)
	if m.runtimeOptionsProvider != nil {
		runtimeOptions := m.runtimeOptionsProvider()
		if runtimeOptions.ClaudeHaikuFallbackModels != nil {
			selector = models.NewSelectorWithConfig(models.SelectorConfig{
				ClaudeHaikuFallbackModels: runtimeOptions.ClaudeHaikuFallbackModels,
			})
		}
		if runtimeOptions.ReasoningPolicies != nil {
			reasoningPolicies = cloneReasoningPolicies(runtimeOptions.ReasoningPolicies)
		}
	}
	transform.RewriteModel(req, rc, m.catalog, selector)
	transform.SelectTargetEndpoint(req, rc)
	ctx.Request = withRequestContext(req, rc)

	codec := m.codec
	modelName := strings.TrimSpace(rc.Info.MappedModel)
	if modelName == "" {
		modelName = strings.TrimSpace(rc.Info.Model)
	}
	supportedEfforts := cloneReasoningEfforts(rc.Info.SupportedReasoningEffort)
	policyForChat, _ := reasoning.MatchPolicy(reasoningPolicies, modelName, reasoning.TargetChat)
	policyForResponses, _ := reasoning.MatchPolicy(reasoningPolicies, modelName, reasoning.TargetResponses)

	codec.MessagesToChatRequest = func(body []byte) ([]byte, bool) {
		return transform.MessagesToChatRequestWithOptions(body, transform.MessagesReasoningOptions{
			PolicyEffort:             policyForChat,
			SupportedReasoningEffort: supportedEfforts,
		})
	}
	codec.MessagesToResponsesRequest = func(body []byte) ([]byte, bool) {
		return transform.MessagesToResponsesRequestWithOptions(body, transform.MessagesReasoningOptions{
			PolicyEffort:             policyForResponses,
			SupportedReasoningEffort: supportedEfforts,
		})
	}

	resp, err := transform.ApplyEndpointTransform(ctx.Request, rc, codec, func(req *http.Request) (*http.Response, error) {
		ctx.Request = req
		rc, ok := middleware.RequestContextFrom(req.Context())
		if !ok || rc == nil {
			rc = ensureRequestContext(req)
			ctx.Request = withRequestContext(req, rc)
		}
		transform.ApplyUpstreamPath(req, rc, m.pathMapping)
		return next()
	})
	if err != nil {
		return nil, fmt.Errorf("apply endpoint transform: %w", err)
	}
	return resp, nil
}

func cloneReasoningEfforts(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

func cloneReasoningPolicies(items []reasoning.Policy) []reasoning.Policy {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]reasoning.Policy, len(items))
	copy(cloned, items)
	return cloned
}

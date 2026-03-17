package flow

import (
	"fmt"
	"net/http"
	"strings"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/reasoning"
	endpointtransform "copilot-proxy/internal/runtime/endpoint/transform"
	models "copilot-proxy/internal/runtime/model"
)

type RuntimeOptions struct {
	ClaudeHaikuFallbackModels []string
	ReasoningPolicies         []reasoning.Policy
}

type ResolvedModel struct {
	ID                       string
	Endpoints                []string
	SupportedReasoningEffort []string
}

func ApplyCatalogEndpointTransform(
	req *http.Request,
	rc *middleware.RequestContext,
	catalog models.Catalog,
	pathMapping map[string]string,
	options RuntimeOptions,
	forward func(*http.Request, *middleware.RequestContext) (*http.Response, error),
) (*http.Response, error) {
	if req == nil {
		return forward(req, rc)
	}
	if rc == nil {
		rc = EnsureRequestContext(req)
	}

	selector := models.NewSelector()
	if options.ClaudeHaikuFallbackModels != nil {
		selector = models.NewSelectorWithConfig(models.SelectorConfig{
			ClaudeHaikuFallbackModels: options.ClaudeHaikuFallbackModels,
		})
	}

	endpointtransform.RewriteModel(req, rc, catalog, selector)
	endpointtransform.SelectTargetEndpoint(req, rc)
	req = WithRequestContext(req, rc)

	modelName := strings.TrimSpace(rc.Info.MappedModel)
	if modelName == "" {
		modelName = strings.TrimSpace(rc.Info.Model)
	}
	codec := BuildEndpointCodec(options.ReasoningPolicies, modelName, rc.Info.SupportedReasoningEffort)
	return ExecuteEndpointTransform(req, rc, pathMapping, codec, forward)
}

func ExecuteEndpointTransform(
	req *http.Request,
	rc *middleware.RequestContext,
	pathMapping map[string]string,
	codec endpointtransform.EndpointCodec,
	forward func(*http.Request, *middleware.RequestContext) (*http.Response, error),
) (*http.Response, error) {
	resp, err := endpointtransform.ApplyEndpointTransform(req, rc, codec, func(nextReq *http.Request) (*http.Response, error) {
		updatedRC, ok := middleware.RequestContextFrom(nextReq.Context())
		if !ok || updatedRC == nil {
			updatedRC = EnsureRequestContext(nextReq)
			nextReq = WithRequestContext(nextReq, updatedRC)
		}
		endpointtransform.ApplyUpstreamPath(nextReq, updatedRC, pathMapping)
		return forward(nextReq, updatedRC)
	})
	if err != nil {
		return nil, fmt.Errorf("apply endpoint transform: %w", err)
	}
	return resp, nil
}

func BuildEndpointCodec(
	policies []reasoning.Policy,
	modelID string,
	supportedEfforts []string,
) endpointtransform.EndpointCodec {
	policyForChat, _ := reasoning.MatchPolicy(policies, modelID, reasoning.TargetChat)
	policyForResponses, _ := reasoning.MatchPolicy(policies, modelID, reasoning.TargetResponses)
	return endpointtransform.EndpointCodec{
		MessagesToChatRequest: func(body []byte) ([]byte, bool) {
			return endpointtransform.MessagesToChatRequestWithOptions(body, endpointtransform.MessagesReasoningOptions{
				PolicyEffort:             policyForChat,
				SupportedReasoningEffort: CloneStrings(supportedEfforts),
			})
		},
		ChatToMessagesResponse: endpointtransform.ChatToMessagesResponse,
		ChatSSEToMessages:      endpointtransform.TranslateChatSSEToMessages,
		MessagesToResponsesRequest: func(body []byte) ([]byte, bool) {
			return endpointtransform.MessagesToResponsesRequestWithOptions(body, endpointtransform.MessagesReasoningOptions{
				PolicyEffort:             policyForResponses,
				SupportedReasoningEffort: CloneStrings(supportedEfforts),
			})
		},
		ResponsesToMessagesResponse: endpointtransform.ResponsesToMessagesResponse,
		ResponsesSSEToMessages:      endpointtransform.TranslateResponsesSSEToMessages,
	}
}

func RewriteMappedModelBody(path string, body []byte, rawModelID, mappedModelID string) ([]byte, bool) {
	if strings.TrimSpace(rawModelID) == "" || strings.TrimSpace(mappedModelID) == "" {
		return body, false
	}
	if strings.EqualFold(strings.TrimSpace(rawModelID), strings.TrimSpace(mappedModelID)) {
		return body, false
	}
	return endpointtransform.RewriteModelInBody(path, body, mappedModelID)
}

func ApplyResolvedModelInfo(
	rc *middleware.RequestContext,
	sourceLocalPath string,
	info middleware.RequestInfo,
	rawModelID string,
	resolved ResolvedModel,
) middleware.RequestInfo {
	if rc == nil {
		return info
	}
	if rc.LocalPath == "" {
		rc.LocalPath = sourceLocalPath
	}
	if rc.SourceLocalPath == "" {
		rc.SourceLocalPath = sourceLocalPath
	}
	if info.Model == "" {
		info.Model = strings.TrimSpace(rawModelID)
	}
	if mappedModelID := strings.TrimSpace(resolved.ID); mappedModelID != "" {
		info.MappedModel = mappedModelID
	} else {
		info.MappedModel = info.Model
	}
	info.SelectedModelEndpoints = CloneStrings(resolved.Endpoints)
	info.SupportedReasoningEffort = CloneStrings(resolved.SupportedReasoningEffort)
	rc.Info = info
	rc.TargetUpstreamPath = endpointtransform.PickTargetEndpoint(sourceLocalPath, resolved.Endpoints)
	return info
}

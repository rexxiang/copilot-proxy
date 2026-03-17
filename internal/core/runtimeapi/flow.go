package runtimeapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"copilot-proxy/internal/config"
	endpointtransform "copilot-proxy/internal/core/endpoint/transform"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/reasoning"
)

func (r *Runtime) doExecuteUpstream(
	ctx context.Context,
	upstreamReq *http.Request,
	settings config.Settings,
	rawModelID string,
	accountRef string,
	resolvedModel ModelInfo,
) (*http.Response, error) {
	if upstreamReq == nil {
		return nil, errors.New("upstream request is required")
	}
	req := upstreamReq.Clone(ctx)

	sourceLocalPath := req.URL.Path
	requestBody, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	_ = req.Body.Close()

	info := middleware.ParseRequestByPathWithOptions(sourceLocalPath, requestBody, middleware.ParseOptions{
		MessagesAgentDetectionRequestMode: settings.MessagesAgentDetectionRequestMode,
	})
	if info.Model == "" {
		info.Model = strings.TrimSpace(rawModelID)
	}
	if mappedModelID := strings.TrimSpace(resolvedModel.ID); mappedModelID != "" {
		info.MappedModel = mappedModelID
	} else {
		info.MappedModel = info.Model
	}
	info.SelectedModelEndpoints = cloneStrings(resolvedModel.Endpoints)
	info.SupportedReasoningEffort = cloneStrings(resolvedModel.SupportedReasoningEffort)

	if rewrittenBody, changed := rewriteMappedModelBody(sourceLocalPath, requestBody, rawModelID, info.MappedModel); changed {
		requestBody = rewrittenBody
	}
	setRequestBody(req, requestBody)

	rc := &middleware.RequestContext{
		LocalPath:          sourceLocalPath,
		SourceLocalPath:    sourceLocalPath,
		TargetUpstreamPath: endpointtransform.PickTargetEndpoint(sourceLocalPath, resolvedModel.Endpoints),
		Account:            config.Account{User: accountRef},
		Info:               info,
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), rc))

	stripClientXHeaders(req.Header)
	applyRuntimeHeaders(req.Header, info, settings)

	codec := runtimeEndpointCodec(settings, info.MappedModel, info.SupportedReasoningEffort)
	return endpointtransform.ApplyEndpointTransform(req, rc, codec, func(nextReq *http.Request) (*http.Response, error) {
		endpointtransform.ApplyUpstreamPath(nextReq, rc, config.PathMapping)
		if r.upstreamDo != nil {
			return r.upstreamDo(ctx, nextReq)
		}
		return r.doUpstreamRequest(ctx, nextReq, settings)
	})
}

func runtimeEndpointCodec(settings config.Settings, modelID string, supportedEfforts []string) endpointtransform.EndpointCodec {
	policies, _ := reasoning.EffectivePoliciesFromMap(settings.ReasoningPoliciesMap)
	policyForChat, _ := reasoning.MatchPolicy(policies, modelID, reasoning.TargetChat)
	policyForResponses, _ := reasoning.MatchPolicy(policies, modelID, reasoning.TargetResponses)
	return endpointtransform.EndpointCodec{
		MessagesToChatRequest: func(body []byte) ([]byte, bool) {
			return endpointtransform.MessagesToChatRequestWithOptions(body, endpointtransform.MessagesReasoningOptions{
				PolicyEffort:             policyForChat,
				SupportedReasoningEffort: cloneStrings(supportedEfforts),
			})
		},
		ChatToMessagesResponse: endpointtransform.ChatToMessagesResponse,
		ChatSSEToMessages:      endpointtransform.TranslateChatSSEToMessages,
		MessagesToResponsesRequest: func(body []byte) ([]byte, bool) {
			return endpointtransform.MessagesToResponsesRequestWithOptions(body, endpointtransform.MessagesReasoningOptions{
				PolicyEffort:             policyForResponses,
				SupportedReasoningEffort: cloneStrings(supportedEfforts),
			})
		},
		ResponsesToMessagesResponse: endpointtransform.ResponsesToMessagesResponse,
		ResponsesSSEToMessages:      endpointtransform.TranslateResponsesSSEToMessages,
	}
}

func rewriteMappedModelBody(path string, body []byte, rawModelID, mappedModelID string) ([]byte, bool) {
	if strings.TrimSpace(rawModelID) == "" || strings.TrimSpace(mappedModelID) == "" {
		return body, false
	}
	if strings.EqualFold(strings.TrimSpace(rawModelID), strings.TrimSpace(mappedModelID)) {
		return body, false
	}
	return endpointtransform.RewriteModelInBody(path, body, mappedModelID)
}

func setRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func stripClientXHeaders(headers http.Header) {
	for key := range headers {
		if strings.HasPrefix(strings.ToLower(key), "x-") {
			headers.Del(key)
		}
	}
}

func applyRuntimeHeaders(headers http.Header, info middleware.RequestInfo, settings config.Settings) {
	for key, value := range settings.RequiredHeadersWithDefaults() {
		if headers.Get(key) == "" {
			headers.Set(key, value)
		}
	}
	if info.IsAgent {
		headers.Set("X-Initiator", "agent")
	} else {
		headers.Set("X-Initiator", "user")
	}
	if info.IsVision {
		headers.Set("Copilot-Vision-Request", "true")
	}
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

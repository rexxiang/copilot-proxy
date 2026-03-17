package runtimeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"copilot-proxy/internal/config"
	endpointflow "copilot-proxy/internal/core/endpoint/flow"
	"copilot-proxy/internal/core/runtimeconfig"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/reasoning"
)

func (r *Runtime) doExecuteUpstream(
	ctx context.Context,
	upstreamReq *http.Request,
	settings runtimeconfig.Config,
	rawModelID string,
	accountRef string,
	resolvedModel ModelInfo,
) (*http.Response, error) {
	if upstreamReq == nil {
		return nil, errors.New("upstream request is required")
	}
	req := upstreamReq.Clone(ctx)

	sourceLocalPath := req.URL.Path
	rc := endpointflow.EnsureRequestContext(req)
	requestBody, info, err := endpointflow.ParseRequest(req, sourceLocalPath, middleware.ParseOptions{
		MessagesAgentDetectionRequestMode: settings.MessagesAgentDetectionRequestMode,
	})
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	info = endpointflow.ApplyResolvedModelInfo(rc, sourceLocalPath, info, rawModelID, endpointflow.ResolvedModel{
		ID:                       resolvedModel.ID,
		Endpoints:                resolvedModel.Endpoints,
		SupportedReasoningEffort: resolvedModel.SupportedReasoningEffort,
	})
	if rewrittenBody, changed := endpointflow.RewriteMappedModelBody(sourceLocalPath, requestBody, rawModelID, info.MappedModel); changed {
		requestBody = rewrittenBody
	}
	endpointflow.RestoreRequestBody(req, requestBody)
	rc.Account = config.Account{User: accountRef}
	req = endpointflow.WithRequestContext(req, rc)

	endpointflow.StripClientXHeaders(req.Header)
	endpointflow.ApplyStaticHeaders(req.Header, settings.RequiredHeadersWithDefaults(), false)
	endpointflow.ApplyDynamicHeaders(req.Header, info)

	codec := endpointflow.BuildEndpointCodec(resolvedPolicies(settings), info.MappedModel, info.SupportedReasoningEffort)
	return endpointflow.ExecuteEndpointTransform(req, rc, config.PathMapping, codec, func(nextReq *http.Request, _ *middleware.RequestContext) (*http.Response, error) {
		if r.upstreamDo != nil {
			return r.upstreamDo(ctx, nextReq)
		}
		return r.doUpstreamRequest(ctx, nextReq, settings)
	})
}

func resolvedPolicies(settings runtimeconfig.Config) []reasoning.Policy {
	policies, _ := reasoning.EffectivePoliciesFromMap(settings.ReasoningPoliciesMap)
	return policies
}

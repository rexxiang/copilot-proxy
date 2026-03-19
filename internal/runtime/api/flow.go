package api

import (
	"context"
	"copilot-proxy/internal/runtime/reasoning"
	"errors"
	"fmt"
	"net/http"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
	protocolpaths "copilot-proxy/internal/runtime/protocol/paths"
	requestctx "copilot-proxy/internal/runtime/request"
)

var defaultPathMapping = protocolpaths.DefaultPathMapping()

func (r *Engine) doExecuteUpstream(
	ctx context.Context,
	upstreamReq *http.Request,
	settings runtimeconfig.RuntimeSettings,
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
	requestBody, info, err := endpointflow.ParseRequest(req, sourceLocalPath, requestctx.ParseOptions{
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
	rc.AccountRef = accountRef
	req = endpointflow.WithRequestContext(req, rc)

	endpointflow.StripClientXHeaders(req.Header)
	endpointflow.ApplyStaticHeaders(req.Header, settings.RequiredHeadersWithDefaults(), false)
	endpointflow.ApplyDynamicHeaders(req.Header, info)

	codec := endpointflow.BuildEndpointCodec(resolvedPolicies(settings), info.MappedModel, info.SupportedReasoningEffort)
	return endpointflow.ExecuteEndpointTransform(req, rc, defaultPathMapping, codec, func(nextReq *http.Request, _ *requestctx.RequestContext) (*http.Response, error) {
		upstreamCtx := nextReq.Context()
		if r.upstreamDo != nil {
			return r.upstreamDo(upstreamCtx, nextReq)
		}
		return r.doUpstreamRequest(upstreamCtx, nextReq, settings)
	})
}

func resolvedPolicies(settings runtimeconfig.RuntimeSettings) []reasoning.Policy {
	policies, _ := reasoning.EffectivePoliciesFromMap(settings.ReasoningPoliciesMap)
	return policies
}

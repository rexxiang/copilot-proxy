package upstream

import (
	"net/http"

	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
	"copilot-proxy/internal/runtime/httpjson"
	requestctx "copilot-proxy/internal/runtime/request"
)

func ensureRequestContext(req *http.Request) *requestctx.RequestContext {
	return endpointflow.EnsureRequestContext(req)
}

func withRequestContext(req *http.Request, rc *requestctx.RequestContext) *http.Request {
	return endpointflow.WithRequestContext(req, rc)
}

func jsonErrorResponse(req *http.Request, status int, message string) *http.Response {
	return httpjson.ErrorResponse(req, status, message)
}

package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
)

// StripXHeadersMiddleware removes client-provided x-* headers before all upstream processing.
type StripXHeadersMiddleware struct{}

// NewStripXHeaders builds middleware that drops incoming x-* headers.
func NewStripXHeaders() StripXHeadersMiddleware {
	return StripXHeadersMiddleware{}
}

func (m StripXHeadersMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}
	endpointflow.StripClientXHeaders(ctx.Request.Header)
	return next()
}

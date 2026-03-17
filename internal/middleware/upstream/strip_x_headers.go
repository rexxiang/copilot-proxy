package upstream

import (
	"net/http"

	endpointflow "copilot-proxy/internal/core/endpoint/flow"
	"copilot-proxy/internal/middleware"
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

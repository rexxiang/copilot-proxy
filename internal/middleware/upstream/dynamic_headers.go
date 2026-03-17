package upstream

import (
	"net/http"

	endpointflow "copilot-proxy/internal/core/endpoint/flow"
	"copilot-proxy/internal/middleware"
)

// DynamicHeadersMiddleware sets x-initiator and Copilot-Vision-Request.
type DynamicHeadersMiddleware struct{}

// NewDynamicHeaders builds a dynamic headers middleware.
func NewDynamicHeaders() DynamicHeadersMiddleware {
	return DynamicHeadersMiddleware{}
}

func (m DynamicHeadersMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	req := ctx.Request
	rc, ok := middleware.RequestContextFrom(req.Context())
	if !ok || rc == nil {
		return next()
	}

	endpointflow.ApplyDynamicHeaders(req.Header, rc.Info)
	return next()
}

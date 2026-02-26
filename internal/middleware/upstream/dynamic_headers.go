package upstream

import (
	"net/http"

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

	if rc.Info.IsAgent {
		req.Header.Set("X-Initiator", "agent")
	} else {
		req.Header.Set("X-Initiator", "user")
	}

	if rc.Info.IsVision {
		req.Header.Set("Copilot-Vision-Request", "true")
	}

	return next()
}

package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// ContextInitMiddleware ensures RequestContext exists with defaults.
type ContextInitMiddleware struct{}

// NewContextInit builds a context initialization middleware.
func NewContextInit() ContextInitMiddleware {
	return ContextInitMiddleware{}
}

func (m ContextInitMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}
	rc := ensureRequestContext(ctx.Request)
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

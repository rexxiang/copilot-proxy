package upstream

import (
	"context"
	"net/http"
	"time"

	"copilot-proxy/internal/middleware"
	requestctx "copilot-proxy/internal/runtime/request"
)

// RequestTimeoutMiddleware ensures the request context has at least the configured timeout.
type RequestTimeoutMiddleware struct {
	timeout time.Duration
}

// NewRequestTimeout builds upstream timeout middleware.
func NewRequestTimeout(timeout time.Duration) RequestTimeoutMiddleware {
	return RequestTimeoutMiddleware{timeout: timeout}
}

func (m RequestTimeoutMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	rc := ensureRequestContext(ctx.Request)
	if m.timeout <= 0 {
		ctx.Request = withRequestContext(ctx.Request, rc)
		return next()
	}

	reqCtx := ctx.Request.Context()
	if deadline, ok := reqCtx.Deadline(); ok {
		nextDeadline := time.Now().Add(m.timeout)
		if deadline.Before(nextDeadline) {
			ctx.Request = withRequestContext(ctx.Request, rc)
			return next()
		}
	}

	ctxToUse, cancel := context.WithTimeout(reqCtx, m.timeout)
	defer cancel()
	ctxToUse = requestctx.WithRequestContext(ctxToUse, rc)
	ctx.Request = ctx.Request.WithContext(ctxToUse)
	return next()
}

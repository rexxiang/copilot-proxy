package middleware

import (
	"context"
	requestctx "copilot-proxy/internal/runtime/request"
)

// RequestInfo aliases runtime request metadata during migration.
type RequestInfo = requestctx.RequestInfo

// RequestContext aliases runtime request context during migration.
type RequestContext = requestctx.RequestContext

// WithRequestContext stores RequestContext in context.
func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return requestctx.WithRequestContext(ctx, rc)
}

// RequestContextFrom retrieves RequestContext from context.
func RequestContextFrom(ctx context.Context) (*RequestContext, bool) {
	return requestctx.RequestContextFrom(ctx)
}

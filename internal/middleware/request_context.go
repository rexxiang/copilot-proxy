package middleware

import (
	"context"
	"time"

	"copilot-proxy/internal/runtime/config"
)

// RequestInfo contains metadata extracted from the request body.
type RequestInfo struct {
	IsVision                 bool     // Request contains image content
	IsAgent                  bool     // Request initiated by agent (not user)
	Model                    string   // Model name from request body
	MappedModel              string   // Model name after mapping
	SelectedModelEndpoints   []string // Endpoints from selector-chosen model (raw catalog values, not normalized)
	SupportedReasoningEffort []string // Model-supported reasoning effort levels (low/medium/high)
}

type requestContextKey struct{}

// RequestContext holds per-request data shared across middleware layers.
type RequestContext struct {
	ID                 string
	LocalPath          string // Local path used by middleware internals
	SourceLocalPath    string // Frozen original local path from request entry
	TargetUpstreamPath string // Final selected upstream endpoint path
	Account            config.Account
	Token              string
	Info               RequestInfo
	Body               []byte
	Headers            map[string]string
	Start              time.Time
	RetryAttempt       bool
	TokenInvalidated   bool
}

// WithRequestContext stores RequestContext in context.
func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	if rc == nil {
		return ctx
	}
	return context.WithValue(ctx, requestContextKey{}, rc)
}

// RequestContextFrom retrieves RequestContext from context.
func RequestContextFrom(ctx context.Context) (*RequestContext, bool) {
	rc, ok := ctx.Value(requestContextKey{}).(*RequestContext)
	return rc, ok
}

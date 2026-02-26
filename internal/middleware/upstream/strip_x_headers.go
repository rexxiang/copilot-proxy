package upstream

import (
	"net/http"
	"strings"

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
	for key := range ctx.Request.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-") {
			ctx.Request.Header.Del(key)
		}
	}
	return next()
}

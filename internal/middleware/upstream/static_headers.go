package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
)

// StaticHeadersMiddleware sets required static headers.
type StaticHeadersMiddleware struct {
	headers         map[string]string
	headersProvider func() map[string]string
}

// NewStaticHeaders builds a static headers middleware.
func NewStaticHeaders(headers map[string]string) StaticHeadersMiddleware {
	return StaticHeadersMiddleware{headers: headers}
}

// NewStaticHeadersProvider builds a headers middleware backed by a runtime provider.
func NewStaticHeadersProvider(provider func() map[string]string) StaticHeadersMiddleware {
	return StaticHeadersMiddleware{headersProvider: provider}
}

func (m StaticHeadersMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	req := ctx.Request
	headers := m.headers
	if m.headersProvider != nil {
		headers = m.headersProvider()
	}
	endpointflow.ApplyStaticHeaders(req.Header, headers, true)
	return next()
}

package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// StaticHeadersMiddleware sets required static headers.
type StaticHeadersMiddleware struct {
	headers map[string]string
}

// NewStaticHeaders builds a static headers middleware.
func NewStaticHeaders(headers map[string]string) StaticHeadersMiddleware {
	return StaticHeadersMiddleware{headers: headers}
}

func (m StaticHeadersMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	req := ctx.Request
	for key, value := range m.headers {
		req.Header.Set(key, value)
	}
	return next()
}

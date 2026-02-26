package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// TokenInjectionMiddleware injects Authorization header from context.
type TokenInjectionMiddleware struct{}

// NewTokenInjection builds a token injection middleware.
func NewTokenInjection() TokenInjectionMiddleware {
	return TokenInjectionMiddleware{}
}

func (m TokenInjectionMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	req := ctx.Request
	if rc, ok := middleware.RequestContextFrom(req.Context()); ok && rc != nil && rc.Token != "" {
		req.Header.Set("Authorization", "Bearer "+rc.Token)
	}
	return next()
}

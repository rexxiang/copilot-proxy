package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/token"
)

// TokenMiddleware fetches token and stores it in RequestContext.
type TokenMiddleware struct{}

// NewToken builds a token middleware.
func NewToken() TokenMiddleware {
	return TokenMiddleware{}
}

func (m TokenMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	rc := ensureRequestContext(ctx.Request)
	tokenValue, err := token.Resolve(ctx.Request.Context(), rc.Account)
	if err != nil {
		return writeTokenErrorResponse(ctx.Request, err), nil
	}
	rc.Token = tokenValue
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

func writeTokenErrorResponse(req *http.Request, err error) *http.Response {
	if isTimeoutRequestError(err) {
		return jsonErrorResponse(req, http.StatusGatewayTimeout, "request timed out")
	}
	return jsonErrorResponse(req, http.StatusBadGateway, "failed to get token")
}

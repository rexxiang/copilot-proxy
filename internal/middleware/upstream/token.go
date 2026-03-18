package upstream

import (
	"errors"
	"net/http"

	"copilot-proxy/internal/middleware"
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
	if rc.AccountToken == "" {
		return writeTokenErrorResponse(ctx.Request, errMissingGitHubToken), nil
	}
	rc.Token = rc.AccountToken
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

var errMissingGitHubToken = errors.New("missing GitHub token")

func writeTokenErrorResponse(req *http.Request, err error) *http.Response {
	if isTimeoutRequestError(err) {
		return jsonErrorResponse(req, http.StatusGatewayTimeout, "request timed out")
	}
	return jsonErrorResponse(req, http.StatusBadGateway, "failed to get token")
}

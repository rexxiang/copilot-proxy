package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// TokenConfig controls token fetching behavior.
type TokenConfig struct {
	Provider middleware.TokenProvider
}

// TokenMiddleware fetches token and stores it in RequestContext.
type TokenMiddleware struct {
	cfg TokenConfig
}

// NewToken builds a token middleware.
func NewToken(cfg TokenConfig) TokenMiddleware {
	return TokenMiddleware{cfg: cfg}
}

func (m TokenMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}
	if m.cfg.Provider == nil {
		return jsonErrorResponse(ctx.Request, http.StatusBadGateway, "failed to get token"), nil
	}

	rc := ensureRequestContext(ctx.Request)
	token, err := m.cfg.Provider.GetToken(ctx.Request.Context(), rc.Account)
	if err != nil {
		return writeTokenErrorResponse(ctx.Request, err), nil
	}
	rc.Token = token
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

func writeTokenErrorResponse(req *http.Request, err error) *http.Response {
	if isTimeoutRequestError(err) {
		return jsonErrorResponse(req, http.StatusGatewayTimeout, "request timed out")
	}
	return jsonErrorResponse(req, http.StatusBadGateway, "failed to get token")
}

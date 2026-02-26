package upstream

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// CaptureDebugMiddleware stores request headers for debug logging.
type CaptureDebugMiddleware struct{}

// NewCaptureDebug builds request debug capture middleware.
func NewCaptureDebug() CaptureDebugMiddleware {
	return CaptureDebugMiddleware{}
}

func (m CaptureDebugMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	rc := ensureRequestContext(ctx.Request)
	headers := make(map[string]string)
	for k, v := range ctx.Request.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	rc.Headers = headers
	ctx.Request = withRequestContext(ctx.Request, rc)
	return next()
}

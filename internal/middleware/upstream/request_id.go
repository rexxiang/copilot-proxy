package upstream

import (
	"fmt"
	"net/http"
	"time"

	"copilot-proxy/internal/middleware"
)

// RequestIDMiddleware assigns and propagates request IDs.
type RequestIDMiddleware struct{}

// NewRequestID builds a request ID middleware.
func NewRequestID() RequestIDMiddleware {
	return RequestIDMiddleware{}
}

func (m RequestIDMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil {
		return next()
	}

	rc := ensureRequestContext(ctx.Request)
	rc.ID = generateRequestID()
	ctx.Request.Header.Set("X-Request-Id", rc.ID)
	ctx.Request = withRequestContext(ctx.Request, rc)

	resp, err := next()
	if resp != nil {
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Set("X-Request-Id", rc.ID)
	}
	return resp, err
}

func generateRequestID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("%x-%x", now.UnixNano(), time.Now().UnixNano())
}

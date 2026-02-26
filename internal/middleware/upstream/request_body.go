package upstream

import (
	"bytes"
	"io"
	"net/http"

	"copilot-proxy/internal/middleware"
)

// ParseRequestBodyMiddleware reads request body and stores info for downstream middleware.
type ParseRequestBodyMiddleware struct{}

// NewParseRequestBody builds request parsing middleware.
func NewParseRequestBody() ParseRequestBodyMiddleware {
	return ParseRequestBodyMiddleware{}
}

func (m ParseRequestBodyMiddleware) Handle(ctx *middleware.Context, next middleware.Next) (*http.Response, error) {
	if ctx == nil || ctx.Request == nil || ctx.Request.Method != http.MethodPost {
		return next()
	}

	rc := ensureRequestContext(ctx.Request)
	req := ctx.Request
	if req.Body == nil {
		ctx.Request = withRequestContext(req, rc)
		return next()
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		ctx.Request = withRestoredBodyAndContext(req, bodyBytes, rc)
		return next()
	}

	if len(bodyBytes) == 0 {
		ctx.Request = withRestoredBodyAndContext(req, bodyBytes, rc)
		return next()
	}

	info := middleware.ParseRequestByPath(req.URL.Path, bodyBytes)
	rc.Body = bodyBytes
	rc.Info = info

	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	req.ContentLength = int64(len(bodyBytes))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}

	ctx.Request = withRequestContext(req, rc)
	return next()
}

func withRestoredBodyAndContext(
	req *http.Request,
	body []byte,
	rc *middleware.RequestContext,
) *http.Request {
	if req == nil {
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return withRequestContext(req, rc)
}

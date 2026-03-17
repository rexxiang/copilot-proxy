package flow

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"copilot-proxy/internal/runtime/config"
	requestctx "copilot-proxy/internal/runtime/request"
)

func EnsureRequestContext(req *http.Request) *requestctx.RequestContext {
	if req == nil {
		return ensureRequestContextDefaults(req, &requestctx.RequestContext{Start: time.Now()})
	}

	if rc, ok := requestctx.RequestContextFrom(req.Context()); ok && rc != nil {
		return ensureRequestContextDefaults(req, rc)
	}
	return ensureRequestContextDefaults(req, &requestctx.RequestContext{})
}

func ensureRequestContextDefaults(req *http.Request, rc *requestctx.RequestContext) *requestctx.RequestContext {
	if rc == nil {
		rc = new(requestctx.RequestContext)
	}
	if req != nil && rc.LocalPath == "" {
		rc.LocalPath = req.URL.Path
	}
	if rc.SourceLocalPath == "" {
		rc.SourceLocalPath = rc.LocalPath
	}
	if rc.Start.IsZero() {
		rc.Start = time.Now()
	}
	if rc.Account.User == "" && rc.Account.GhToken == "" {
		rc.Account = config.Account{}
	}
	return rc
}

func WithRequestContext(req *http.Request, rc *requestctx.RequestContext) *http.Request {
	if req == nil {
		return nil
	}
	return req.WithContext(requestctx.WithRequestContext(req.Context(), rc))
}

func ReadAndRestoreRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	RestoreRequestBody(req, body)
	if err != nil {
		return body, err
	}
	return body, nil
}

func RestoreRequestBody(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func ParseRequest(req *http.Request, path string, options requestctx.ParseOptions) ([]byte, requestctx.RequestInfo, error) {
	if req == nil {
		return nil, requestctx.RequestInfo{}, nil
	}
	body, err := ReadAndRestoreRequestBody(req)
	if err != nil {
		return body, requestctx.RequestInfo{}, err
	}
	if len(body) == 0 {
		return body, requestctx.RequestInfo{}, nil
	}
	if path == "" {
		path = req.URL.Path
	}
	return body, requestctx.ParseRequestByPathWithOptions(path, body, options), nil
}

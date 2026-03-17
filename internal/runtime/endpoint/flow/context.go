package flow

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
)

func EnsureRequestContext(req *http.Request) *middleware.RequestContext {
	if req == nil {
		return ensureRequestContextDefaults(req, &middleware.RequestContext{Start: time.Now()})
	}

	if rc, ok := middleware.RequestContextFrom(req.Context()); ok && rc != nil {
		return ensureRequestContextDefaults(req, rc)
	}
	return ensureRequestContextDefaults(req, &middleware.RequestContext{})
}

func ensureRequestContextDefaults(req *http.Request, rc *middleware.RequestContext) *middleware.RequestContext {
	if rc == nil {
		rc = new(middleware.RequestContext)
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

func WithRequestContext(req *http.Request, rc *middleware.RequestContext) *http.Request {
	if req == nil {
		return nil
	}
	return req.WithContext(middleware.WithRequestContext(req.Context(), rc))
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

func ParseRequest(req *http.Request, path string, options middleware.ParseOptions) ([]byte, middleware.RequestInfo, error) {
	if req == nil {
		return nil, middleware.RequestInfo{}, nil
	}
	body, err := ReadAndRestoreRequestBody(req)
	if err != nil {
		return body, middleware.RequestInfo{}, err
	}
	if len(body) == 0 {
		return body, middleware.RequestInfo{}, nil
	}
	if path == "" {
		path = req.URL.Path
	}
	return body, middleware.ParseRequestByPathWithOptions(path, body, options), nil
}

package upstream

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
)

func ensureRequestContext(req *http.Request) *middleware.RequestContext {
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

func withRequestContext(req *http.Request, rc *middleware.RequestContext) *http.Request {
	if req == nil {
		return nil
	}
	return req.WithContext(middleware.WithRequestContext(req.Context(), rc))
}

func jsonErrorResponse(req *http.Request, status int, message string) *http.Response {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		payload = []byte(`{"error":"internal error"}`)
	}

	resp := new(http.Response)
	resp.StatusCode = status
	resp.Header = http.Header{"Content-Type": []string{"application/json"}}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Request = req

	return resp
}

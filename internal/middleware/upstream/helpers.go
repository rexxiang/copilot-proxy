package upstream

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"copilot-proxy/internal/middleware"
	endpointflow "copilot-proxy/internal/runtime/endpoint/flow"
)

func ensureRequestContext(req *http.Request) *middleware.RequestContext {
	return endpointflow.EnsureRequestContext(req)
}

func withRequestContext(req *http.Request, rc *middleware.RequestContext) *http.Request {
	return endpointflow.WithRequestContext(req, rc)
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

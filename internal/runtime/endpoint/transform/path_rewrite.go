package transform

import (
	"net/http"

	"copilot-proxy/internal/middleware"
)

// ApplyUpstreamPath rewrites request path to chosen upstream endpoint.
func ApplyUpstreamPath(req *http.Request, rc *middleware.RequestContext, mapping map[string]string) {
	if req == nil {
		return
	}
	if rc != nil && rc.TargetUpstreamPath != "" {
		req.URL.Path = rc.TargetUpstreamPath
		return
	}
	path := req.URL.Path
	if rc != nil {
		if rc.SourceLocalPath != "" {
			path = rc.SourceLocalPath
		} else if rc.LocalPath != "" {
			path = rc.LocalPath
		}
	}
	if upstreamPath, ok := mapping[path]; ok {
		req.URL.Path = upstreamPath
	}
}

package transform

import (
	"net/http"
	"strings"

	requestctx "copilot-proxy/internal/runtime/request"
)

// SelectTargetEndpoint chooses final upstream endpoint based on model capability and source endpoint.
func SelectTargetEndpoint(req *http.Request, rc *requestctx.RequestContext) {
	if req == nil || rc == nil {
		return
	}

	sourceLocalPath := rc.SourceLocalPath
	if sourceLocalPath == "" {
		if rc.LocalPath != "" {
			sourceLocalPath = rc.LocalPath
		} else {
			sourceLocalPath = req.URL.Path
		}
		rc.SourceLocalPath = sourceLocalPath
	}

	if !IsModelRewritePath(sourceLocalPath) {
		return
	}
	// Without a resolved model, keep legacy path behavior and skip endpoint selection.
	if strings.TrimSpace(rc.Info.MappedModel) == "" && strings.TrimSpace(rc.Info.Model) == "" {
		return
	}

	rc.TargetUpstreamPath = PickTargetEndpoint(sourceLocalPath, rc.Info.SelectedModelEndpoints)
}

package transform

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
)

// RewriteModel applies model mapping and stores selected endpoint metadata.
func RewriteModel(req *http.Request, rc *middleware.RequestContext, catalog models.Catalog, selector *models.Selector) {
	path := resolveRewritePath(req, rc)
	if shouldSkipModelRewrite(req, path) {
		return
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		restoreModelRewriteBody(req, bodyBytes)
		return
	}
	info := middleware.ParseRequestByPath(path, bodyBytes)
	if info.Model == "" {
		info.Model = rawModelFromJSON(bodyBytes)
	}
	if info.Model == "" {
		if rc != nil {
			rc.Info.MappedModel = ""
			rc.Info.SelectedModelEndpoints = nil
			rc.Info.SupportedReasoningEffort = nil
		}
		restoreModelRewriteBody(req, bodyBytes)
		return
	}

	if selector == nil {
		selector = models.NewSelector()
	}
	selected, changed := "", false
	selectedEndpoints := []string(nil)
	supportedReasoningEffort := []string(nil)
	if catalog != nil && selector != nil {
		if selectedModel, mapped, found := selector.SelectModelInfo(catalog.GetModels(), info.Model); found {
			selected = selectedModel.ID
			changed = mapped
			selectedEndpoints = cloneStringSlice(selectedModel.Endpoints)
			supportedReasoningEffort = cloneStringSlice(selectedModel.SupportedReasoningEffort)
		}
	}
	if selected != "" {
		info.MappedModel = selected
	} else {
		info.MappedModel = info.Model
	}
	info.SelectedModelEndpoints = selectedEndpoints
	info.SupportedReasoningEffort = supportedReasoningEffort
	if rc != nil {
		rc.Info.MappedModel = info.MappedModel
		rc.Info.SelectedModelEndpoints = cloneStringSlice(info.SelectedModelEndpoints)
		rc.Info.SupportedReasoningEffort = cloneStringSlice(info.SupportedReasoningEffort)
	}
	if changed {
		mapped := selected
		updated, ok := RewriteModelInBody(path, bodyBytes, mapped)
		if ok {
			bodyBytes = updated
			info.Model = mapped
		}
		if rc != nil {
			info.IsAgent = rc.Info.IsAgent
			info.IsVision = rc.Info.IsVision
			rc.Info = info
		}
	}

	restoreModelRewriteBody(req, bodyBytes)
}

func shouldSkipModelRewrite(req *http.Request, path string) bool {
	if req == nil || req.Body == nil {
		return true
	}
	return !IsModelRewritePath(path)
}

func resolveRewritePath(req *http.Request, rc *middleware.RequestContext) string {
	if req == nil {
		return ""
	}
	path := req.URL.Path
	if rc != nil {
		if rc.SourceLocalPath != "" {
			path = rc.SourceLocalPath
		} else if rc.LocalPath != "" {
			path = rc.LocalPath
		}
	}
	return path
}

func restoreModelRewriteBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func rawModelFromJSON(body []byte) string {
	var raw struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	return raw.Model
}

// IsModelRewritePath returns true if the path uses a model-bearing request body.
func IsModelRewritePath(path string) bool {
	switch path {
	case config.ChatCompletionsPath, config.ResponsesPath, config.MessagesPath:
		return true
	default:
		return false
	}
}

// RewriteModelInBody rewrites the model field in the request body for supported paths.
func RewriteModelInBody(path string, body []byte, mapped string) ([]byte, bool) {
	switch path {
	case config.ChatCompletionsPath:
		return rewriteModelField(body, mapped)
	case config.ResponsesPath:
		return rewriteModelField(body, mapped)
	case config.MessagesPath:
		return rewriteModelField(body, mapped)
	default:
		return body, false
	}
}

func rewriteModelField(body []byte, mapped string) ([]byte, bool) {
	var req map[string]any
	if json.Unmarshal(body, &req) != nil {
		return body, false
	}
	req["model"] = mapped
	updated, err := json.Marshal(req)
	if err != nil {
		return body, false
	}
	return updated, true
}

func cloneStringSlice(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

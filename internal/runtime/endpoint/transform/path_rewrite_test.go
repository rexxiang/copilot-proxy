package transform

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
)

func TestPathRewriteUsesTargetUpstreamPathFirst(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.ChatCompletionsPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath:    config.ChatCompletionsPath,
		LocalPath:          config.ChatCompletionsPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	}

	ApplyUpstreamPath(req, rc, config.PathMapping)
	if req.URL.Path != config.UpstreamResponsesPath {
		t.Fatalf("expected rewritten path %q, got %q", config.UpstreamResponsesPath, req.URL.Path)
	}
}

func TestPathRewriteFallsBackToMappingWhenTargetEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.ResponsesPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath: config.ResponsesPath,
		LocalPath:       config.ResponsesPath,
	}

	ApplyUpstreamPath(req, rc, config.PathMapping)
	if req.URL.Path != config.UpstreamResponsesPath {
		t.Fatalf("expected fallback mapping path %q, got %q", config.UpstreamResponsesPath, req.URL.Path)
	}
}

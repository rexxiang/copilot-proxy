package transform

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
)

func TestEndpointSelectUsesSourcePathAndSelectedEndpoints(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.MessagesPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
			SelectedModelEndpoints: []string{
				config.UpstreamResponsesPath,
				config.UpstreamMessagesPath,
			},
		},
	}

	SelectTargetEndpoint(req, rc)
	if rc.TargetUpstreamPath != config.UpstreamMessagesPath {
		t.Fatalf("expected same-endpoint priority %q, got %q", config.UpstreamMessagesPath, rc.TargetUpstreamPath)
	}
}

func TestEndpointSelectFallsBackToChatWhenModelEndpointsMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.ResponsesPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath: config.ResponsesPath,
		LocalPath:       config.ResponsesPath,
		Info: middleware.RequestInfo{
			Model:                  "gpt-4o",
			SelectedModelEndpoints: nil,
		},
	}

	SelectTargetEndpoint(req, rc)
	if rc.TargetUpstreamPath != config.UpstreamResponsesPath {
		t.Fatalf("expected fixed endpoint %q, got %q", config.UpstreamResponsesPath, rc.TargetUpstreamPath)
	}
}

func TestEndpointSelectKeepsChatCompletionsAtSameEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.ChatCompletionsPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath: config.ChatCompletionsPath,
		LocalPath:       config.ChatCompletionsPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
			SelectedModelEndpoints: []string{
				config.UpstreamResponsesPath,
				config.UpstreamMessagesPath,
			},
		},
	}

	SelectTargetEndpoint(req, rc)
	if rc.TargetUpstreamPath != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected fixed endpoint %q, got %q", config.UpstreamChatCompletionsPath, rc.TargetUpstreamPath)
	}
}

func TestEndpointSelectKeepsResponsesAtSameEndpointEvenIfOthersPreferred(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://localhost"+config.ResponsesPath, http.NoBody)
	rc := &middleware.RequestContext{
		SourceLocalPath: config.ResponsesPath,
		LocalPath:       config.ResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
			SelectedModelEndpoints: []string{
				config.UpstreamMessagesPath,
				config.UpstreamChatCompletionsPath,
			},
		},
	}

	SelectTargetEndpoint(req, rc)
	if rc.TargetUpstreamPath != config.UpstreamResponsesPath {
		t.Fatalf("expected fixed endpoint %q, got %q", config.UpstreamResponsesPath, rc.TargetUpstreamPath)
	}
}

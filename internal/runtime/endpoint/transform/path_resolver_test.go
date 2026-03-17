package transform

import (
	"testing"

	"copilot-proxy/internal/runtime/config"
)

func TestLocalToUpstream(t *testing.T) {
	got, ok := LocalToUpstream(config.MessagesPath)
	if !ok {
		t.Fatalf("expected mapping for %q", config.MessagesPath)
	}
	if got != config.UpstreamMessagesPath {
		t.Fatalf("expected %q, got %q", config.UpstreamMessagesPath, got)
	}
}

func TestPickTargetEndpointPrefersCurrentEndpoint(t *testing.T) {
	target := PickTargetEndpoint(config.MessagesPath, []string{
		config.UpstreamResponsesPath,
		config.UpstreamMessagesPath,
	})
	if target != config.UpstreamMessagesPath {
		t.Fatalf("expected current endpoint %q, got %q", config.UpstreamMessagesPath, target)
	}
}

func TestPickTargetEndpointKeepsChatCompletionsFixed(t *testing.T) {
	target := PickTargetEndpoint(config.ChatCompletionsPath, []string{
		config.UpstreamMessagesPath,
		config.UpstreamResponsesPath,
	})
	if target != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected %q, got %q", config.UpstreamChatCompletionsPath, target)
	}

	target = PickTargetEndpoint(config.ChatCompletionsPath, []string{
		config.UpstreamMessagesPath,
	})
	if target != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected %q, got %q", config.UpstreamChatCompletionsPath, target)
	}
}

func TestPickTargetEndpointKeepsResponsesFixed(t *testing.T) {
	target := PickTargetEndpoint(config.ResponsesPath, nil)
	if target != config.UpstreamResponsesPath {
		t.Fatalf("expected fixed %q, got %q", config.UpstreamResponsesPath, target)
	}

	target = PickTargetEndpoint(config.ResponsesPath, []string{"", "/unknown"})
	if target != config.UpstreamResponsesPath {
		t.Fatalf("expected fixed %q for unknown endpoints, got %q", config.UpstreamResponsesPath, target)
	}
}

func TestPickTargetEndpointMessagesFallsBackToChatCompletionsWhenMissingOrUnknown(t *testing.T) {
	target := PickTargetEndpoint(config.MessagesPath, nil)
	if target != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected fallback %q, got %q", config.UpstreamChatCompletionsPath, target)
	}

	target = PickTargetEndpoint(config.MessagesPath, []string{"", "/unknown"})
	if target != config.UpstreamChatCompletionsPath {
		t.Fatalf("expected fallback %q for unknown endpoints, got %q", config.UpstreamChatCompletionsPath, target)
	}
}

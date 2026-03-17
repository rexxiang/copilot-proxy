package config

import (
	"reflect"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ListenAddr != "127.0.0.1:4000" {
		t.Fatalf("unexpected listen addr: %s", cfg.ListenAddr)
	}
	if cfg.UpstreamBase != "https://api.githubcopilot.com" {
		t.Fatalf("unexpected upstream base: %s", cfg.UpstreamBase)
	}
	if cfg.MaxRetries != 3 {
		t.Fatalf("unexpected max retries: %d", cfg.MaxRetries)
	}
	if cfg.RetryBackoff.Duration() != time.Second {
		t.Fatalf("unexpected retry backoff: %s", cfg.RetryBackoff.Duration())
	}
	if !cfg.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages agent detection request mode enabled")
	}
	if !reflect.DeepEqual(cfg.ClaudeHaikuFallbackModels, []string{"gpt-5-mini", "grok-code-fast-1"}) {
		t.Fatalf("unexpected fallback models: %#v", cfg.ClaudeHaikuFallbackModels)
	}
}

func TestRequiredHeadersWithDefaults(t *testing.T) {
	cfg := RuntimeSettings{RequiredHeaders: map[string]string{"editor-version": "custom"}}
	headers := (&cfg).RequiredHeadersWithDefaults()
	if headers["editor-version"] != "custom" {
		t.Fatalf("expected explicit editor-version override")
	}
	if headers["user-agent"] == "" {
		t.Fatalf("expected default user-agent")
	}
	if headers["copilot-integration-id"] == "" {
		t.Fatalf("expected default copilot-integration-id")
	}
}

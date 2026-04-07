package flow

import (
	"net/http"
	"testing"
)

func TestStripClientXHeadersKeepsAllowlistedHeaders(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-GitHub-Api-Version", "2025-05-01")
	headers.Set("x-interaction-type", "conversation-agent")
	headers.Set("X-Interaction-Id", "interaction-1")
	headers.Set("X-Copilot-Account", "alice")
	headers.Set("X-Forwarded-For", "127.0.0.1")
	headers.Set("Openai-Intent", "conversation-agent")

	StripClientXHeaders(headers)

	if got := headers.Get("X-GitHub-Api-Version"); got != "2025-05-01" {
		t.Fatalf("expected X-GitHub-Api-Version to be preserved, got %q", got)
	}
	if got := headers.Get("X-Interaction-Type"); got != "conversation-agent" {
		t.Fatalf("expected X-Interaction-Type to be preserved, got %q", got)
	}
	if got := headers.Get("X-Interaction-Id"); got != "interaction-1" {
		t.Fatalf("expected X-Interaction-Id to be preserved, got %q", got)
	}
	if got := headers.Get("X-Copilot-Account"); got != "" {
		t.Fatalf("expected X-Copilot-Account to be stripped, got %q", got)
	}
	if got := headers.Get("X-Forwarded-For"); got != "" {
		t.Fatalf("expected X-Forwarded-For to be stripped, got %q", got)
	}
	if got := headers.Get("Openai-Intent"); got != "conversation-agent" {
		t.Fatalf("expected Openai-Intent to remain unchanged, got %q", got)
	}
}

func TestStripClientXHeadersTreatsAllowlistAsCaseInsensitive(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-GITHUB-API-VERSION", "2025-05-01")
	headers.Set("X-INTERACTION-TYPE", "conversation-agent")
	headers.Set("X-INTERACTION-ID", "interaction-2")

	StripClientXHeaders(headers)

	if got := headers.Get("X-GitHub-Api-Version"); got != "2025-05-01" {
		t.Fatalf("expected case-insensitive preserve for X-GitHub-Api-Version, got %q", got)
	}
	if got := headers.Get("X-Interaction-Type"); got != "conversation-agent" {
		t.Fatalf("expected case-insensitive preserve for X-Interaction-Type, got %q", got)
	}
	if got := headers.Get("X-Interaction-Id"); got != "interaction-2" {
		t.Fatalf("expected case-insensitive preserve for X-Interaction-Id, got %q", got)
	}
}

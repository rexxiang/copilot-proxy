package cli

import (
	"testing"

	"copilot-proxy/internal/config"
)

func TestCompileRuntimeSettingsSnapshot_ValidatesAndBuilds(t *testing.T) {
	settings := config.DefaultSettings()
	settings.RateLimitSeconds = 2
	settings.MessagesAgentDetectionRequestMode = false
	settings.ClaudeHaikuFallbackModels = []string{" gpt-5-mini ", "", "grok-code-fast-1"}
	settings.ReasoningPoliciesMap = map[string]string{
		"custom-model@chat": "low",
	}

	snapshot, err := compileRuntimeSettingsSnapshot(settings)
	if err != nil {
		t.Fatalf("compileRuntimeSettingsSnapshot error: %v", err)
	}
	if snapshot.MaxRetries != settings.MaxRetries {
		t.Fatalf("unexpected max retries: got %d want %d", snapshot.MaxRetries, settings.MaxRetries)
	}
	if snapshot.RateLimitCooldown.Seconds() != 2 {
		t.Fatalf("unexpected rate limit cooldown: %v", snapshot.RateLimitCooldown)
	}
	if snapshot.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected request mode false in snapshot")
	}
	if got := snapshot.ClaudeHaikuFallbackModels; len(got) != 2 || got[0] != "gpt-5-mini" || got[1] != "grok-code-fast-1" {
		t.Fatalf("unexpected fallback models: %#v", got)
	}
	if len(snapshot.ReasoningPolicies) == 0 {
		t.Fatalf("expected effective reasoning policies to be compiled")
	}
}

func TestCompileRuntimeSettingsSnapshot_RejectsInvalidRateLimit(t *testing.T) {
	settings := config.DefaultSettings()
	settings.RateLimitSeconds = -1

	if _, err := compileRuntimeSettingsSnapshot(settings); err == nil {
		t.Fatalf("expected compileRuntimeSettingsSnapshot to fail for negative rate limit")
	}
}

func TestRuntimeSettingsStore_PublishUpdatesAndClones(t *testing.T) {
	initial := config.DefaultSettings()
	store, err := newRuntimeSettingsStore(initial)
	if err != nil {
		t.Fatalf("newRuntimeSettingsStore error: %v", err)
	}

	next := initial
	next.MessagesAgentDetectionRequestMode = false
	next.RateLimitSeconds = 4
	next.ClaudeHaikuFallbackModels = []string{"grok-code-fast-1"}
	snapshot, err := compileRuntimeSettingsSnapshot(next)
	if err != nil {
		t.Fatalf("compileRuntimeSettingsSnapshot error: %v", err)
	}
	store.Publish(next, snapshot)

	current := store.Current()
	if current.RateLimitSeconds != 4 {
		t.Fatalf("expected current rate limit to update, got %d", current.RateLimitSeconds)
	}
	if current.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected current request mode false")
	}
	current.ClaudeHaikuFallbackModels[0] = "tampered"
	currentAgain := store.Current()
	if currentAgain.ClaudeHaikuFallbackModels[0] != "grok-code-fast-1" {
		t.Fatalf("expected current settings to be cloned on read")
	}

	snap := store.Snapshot()
	snap.ClaudeHaikuFallbackModels[0] = "tampered"
	snapAgain := store.Snapshot()
	if snapAgain.ClaudeHaikuFallbackModels[0] != "grok-code-fast-1" {
		t.Fatalf("expected runtime snapshot to be cloned on read")
	}
}

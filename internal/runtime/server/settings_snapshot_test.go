package server

import (
	"testing"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

func TestCompileRuntimeSettingsSnapshot_ValidatesAndBuilds(t *testing.T) {
	settings := runtimeconfig.Default()
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
	settings := runtimeconfig.Default()
	settings.RateLimitSeconds = -1

	if _, err := compileRuntimeSettingsSnapshot(settings); err == nil {
		t.Fatalf("expected compileRuntimeSettingsSnapshot to fail for negative rate limit")
	}
}

func TestCompileSnapshot_ReturnsClonedSlices(t *testing.T) {
	settings := runtimeconfig.Default()
	settings.ClaudeHaikuFallbackModels = []string{"grok-code-fast-1"}

	snapshot, err := CompileSnapshot(settings)
	if err != nil {
		t.Fatalf("CompileSnapshot error: %v", err)
	}
	snapshot.ClaudeHaikuFallbackModels[0] = "tampered"

	snapshotAgain, err := CompileSnapshot(settings)
	if err != nil {
		t.Fatalf("CompileSnapshot error: %v", err)
	}
	if snapshotAgain.ClaudeHaikuFallbackModels[0] != "grok-code-fast-1" {
		t.Fatalf("expected compiled snapshot to clone fallback models, got %#v", snapshotAgain.ClaudeHaikuFallbackModels)
	}
}

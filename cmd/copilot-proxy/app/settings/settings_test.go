package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

func TestLoadSettingsDefaultWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	current, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if current.ListenAddr != "127.0.0.1:4000" {
		t.Fatalf("unexpected listen addr: %s", current.ListenAddr)
	}
	if current.UpstreamBase != "https://api.githubcopilot.com" {
		t.Fatalf("unexpected upstream base: %s", current.UpstreamBase)
	}
	if !current.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode default true")
	}
	if current.RateLimitSeconds != 0 {
		t.Fatalf("expected rate_limit_seconds default 0, got %d", current.RateLimitSeconds)
	}
	if !reflect.DeepEqual(current.ClaudeHaikuFallbackModels, []string{"gpt-5-mini", "grok-code-fast-1"}) {
		t.Fatalf("unexpected default haiku fallbacks: %#v", current.ClaudeHaikuFallbackModels)
	}
}

func TestSaveLoadSettingsReasoningPoliciesShadowSync(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := DefaultSettings()
	input.ListenAddr = "127.0.0.1:4231"
	input.UpstreamBase = "https://example.com"
	input.MessagesAgentDetectionRequestMode = false
	input.RequiredHeaders = map[string]string{"X-Test": "1"}
	input.MaxRetries = 5
	input.RetryBackoff = NewDuration(2 * time.Second)
	input.RateLimitSeconds = 9
	input.ReasoningPolicies = []ReasoningPolicy{
		{Model: "gpt-5-mini", Target: "responses", Effort: "low"},
		{Model: "grok-code-fast-1", Target: "chat", Effort: "none"},
	}
	input.ClaudeHaikuFallbackModels = []string{"grok-code-fast-1", "gpt-5-mini"}
	input.SyncViewFieldsFromStorage()

	if err := SaveSettings(&input); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	output, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if output.ListenAddr != input.ListenAddr {
		t.Fatalf("unexpected listen addr: got %q want %q", output.ListenAddr, input.ListenAddr)
	}
	if output.UpstreamBase != input.UpstreamBase {
		t.Fatalf("unexpected upstream base: got %q want %q", output.UpstreamBase, input.UpstreamBase)
	}
	if output.RateLimitSeconds != input.RateLimitSeconds {
		t.Fatalf("unexpected rate limit seconds: got %d want %d", output.RateLimitSeconds, input.RateLimitSeconds)
	}
	if output.MessagesAgentDetectionRequestMode != input.MessagesAgentDetectionRequestMode {
		t.Fatalf(
			"unexpected messages_agent_detection_request_mode: got %v want %v",
			output.MessagesAgentDetectionRequestMode,
			input.MessagesAgentDetectionRequestMode,
		)
	}
	if !reflect.DeepEqual(output.ReasoningPolicies, input.ReasoningPolicies) {
		t.Fatalf("unexpected reasoning policies: got %#v want %#v", output.ReasoningPolicies, input.ReasoningPolicies)
	}
	if !reflect.DeepEqual(output.ClaudeHaikuFallbackModels, input.ClaudeHaikuFallbackModels) {
		t.Fatalf("unexpected haiku fallbacks: got %#v want %#v", output.ClaudeHaikuFallbackModels, input.ClaudeHaikuFallbackModels)
	}
}

func TestSaveLoadSettingsExplicitEmptyClaudeHaikuFallbackModels(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := DefaultSettings()
	input.ClaudeHaikuFallbackModels = []string{}
	input.ClaudeHaikuFallbackModelsUI = []HaikuFallbackModel{}

	if err := SaveSettings(&input); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	output, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if output.ClaudeHaikuFallbackModels == nil {
		t.Fatalf("expected explicit empty haiku fallbacks to be preserved, got nil")
	}
	if len(output.ClaudeHaikuFallbackModels) != 0 {
		t.Fatalf("expected explicit empty haiku fallbacks, got %#v", output.ClaudeHaikuFallbackModels)
	}
}

func TestLoadSettingsMessagesAgentDetectionRequestModeDefaultsTrueWhenFieldMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	raw := []byte(`{
  "listen_addr": "127.0.0.1:4000",
  "upstream_base": "https://api.githubcopilot.com"
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}
	if !settings.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode default true when key missing")
	}
}

func TestSettingsJSONDoesNotContainTokenTimeout(t *testing.T) {
	settings := DefaultSettings()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal settings json: %v", err)
	}
	if _, exists := data["token_timeout"]; exists {
		t.Fatalf("token_timeout should not exist in settings json")
	}
	if _, exists := data["upstream_timeout"]; exists {
		t.Fatalf("upstream_timeout should not exist in settings json")
	}
}

func TestRequiredHeadersWithDefaults(t *testing.T) {
	settings := Settings{RequiredHeaders: map[string]string{"editor-version": "custom"}}
	headers := (&settings).RequiredHeadersWithDefaults()
	if headers["editor-version"] != "custom" {
		t.Fatalf("expected explicit editor-version override")
	}
	if headers["user-agent"] == "" {
		t.Fatalf("expected default user-agent")
	}
	if headers["copilot-integration-id"] == "" {
		t.Fatalf("expected integration id")
	}
}

func TestRequiredHeadersWithDefaultsNoDefaultEditorVersion(t *testing.T) {
	headers := (*Settings)(nil).RequiredHeadersWithDefaults()
	if _, ok := headers["editor-version"]; ok {
		t.Fatalf("did not expect default editor-version")
	}
}

func TestRuntimeConfigAdaptersExcludeUIOnlyFields(t *testing.T) {
	input := DefaultSettings()
	input.RequiredHeaders = map[string]string{"editor-version": "v1"}
	input.ReasoningPolicies = []ReasoningPolicy{
		{Model: "gpt-5-mini", Target: "responses", Effort: "high"},
	}
	input.ClaudeHaikuFallbackModels = []string{"grok-code-fast-1", "gpt-5-mini"}
	input.SyncViewFieldsFromStorage()

	cfg := ToRuntimeConfig(input)
	if !reflect.DeepEqual(cfg, runtimeconfig.RuntimeSettings{
		ListenAddr:                        input.ListenAddr,
		UpstreamBase:                      input.UpstreamBase,
		RequiredHeaders:                   map[string]string{"editor-version": "v1"},
		MaxRetries:                        input.MaxRetries,
		RetryBackoff:                      input.RetryBackoff,
		RateLimitSeconds:                  input.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: input.MessagesAgentDetectionRequestMode,
		ReasoningPoliciesMap:              map[string]string{"gpt-5-mini@responses": "high"},
		ClaudeHaikuFallbackModels:         []string{"grok-code-fast-1", "gpt-5-mini"},
	}) {
		t.Fatalf("unexpected runtime config: %#v", cfg)
	}

	roundTrip := FromRuntimeConfig(cfg)
	if len(roundTrip.ReasoningPolicies) != 1 {
		t.Fatalf("expected reasoning policies shadow rows restored, got %#v", roundTrip.ReasoningPolicies)
	}
	if len(roundTrip.ClaudeHaikuFallbackModelsUI) != 2 {
		t.Fatalf("expected fallback UI rows restored, got %#v", roundTrip.ClaudeHaikuFallbackModelsUI)
	}
	if got := roundTrip.ClaudeHaikuFallbackModelsUI[0].Model; got != "grok-code-fast-1" {
		t.Fatalf("unexpected first fallback row: %q", got)
	}
}

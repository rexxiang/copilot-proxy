package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadSettingsDefaultWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if settings.ListenAddr != "127.0.0.1:4000" {
		t.Fatalf("unexpected listen addr: %s", settings.ListenAddr)
	}
	if settings.UpstreamBase != "https://api.githubcopilot.com" {
		t.Fatalf("unexpected upstream base: %s", settings.UpstreamBase)
	}
	if settings.UpstreamTimeout.Duration() != 5*time.Minute {
		t.Fatalf("unexpected upstream timeout: %s", settings.UpstreamTimeout.Duration())
	}
	if !settings.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode default true")
	}
	if settings.RateLimitSeconds != 0 {
		t.Fatalf("expected rate_limit_seconds default 0, got %d", settings.RateLimitSeconds)
	}
	if !reflect.DeepEqual(settings.ClaudeHaikuFallbackModels, []string{"gpt-5-mini", "grok-code-fast-1"}) {
		t.Fatalf("unexpected default haiku fallbacks: %#v", settings.ClaudeHaikuFallbackModels)
	}
}

func TestSaveLoadSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := DefaultSettings()
	input.ListenAddr = "127.0.0.1:1234"
	input.UpstreamBase = "https://example.com"
	input.MessagesAgentDetectionRequestMode = false
	input.RequiredHeaders = map[string]string{
		"X-Test": "1",
	}
	input.UpstreamTimeout = NewDuration(45 * time.Second)
	input.MaxRetries = 5
	input.RetryBackoff = NewDuration(2 * time.Second)
	input.RateLimitSeconds = 9
	input.ClaudeHaikuFallbackModels = []string{"grok-code-fast-1", "gpt-5-mini"}
	input.syncClaudeHaikuFallbackModelsFromStorage()

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

func TestSaveLoadSettingsReasoningPoliciesShadowSync(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := DefaultSettings()
	input.ReasoningPolicies = []ReasoningPolicy{
		{Model: "gpt-5-mini", Target: "responses", Effort: "low"},
		{Model: "grok-code-fast-1", Target: "chat", Effort: "none"},
	}

	if err := SaveSettings(&input); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	output, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if len(output.ReasoningPoliciesMap) != 2 {
		t.Fatalf("expected reasoning policies map persisted, got %#v", output.ReasoningPoliciesMap)
	}
	if output.ReasoningPoliciesMap["gpt-5-mini@responses"] != "low" {
		t.Fatalf("expected map value gpt-5-mini@responses=low, got %#v", output.ReasoningPoliciesMap)
	}
	if len(output.ReasoningPolicies) != 2 {
		t.Fatalf("expected shadow reasoning policies loaded, got %#v", output.ReasoningPolicies)
	}
}

func TestLoadSettings_MessagesAgentDetectionRequestModeDefaultsTrueWhenFieldMissing(t *testing.T) {
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

func TestLoadAuthMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	auth, err := LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth error: %v", err)
	}

	if len(auth.Accounts) != 0 {
		t.Fatalf("expected no accounts")
	}
	if auth.Default != "" {
		t.Fatalf("expected empty default")
	}
}

func TestSaveLoadAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := AuthConfig{
		Default: "user-1",
		Accounts: []Account{
			{User: "user-1", GhToken: "token-1", AppID: "app-1"},
			{User: "user-2", GhToken: "token-2", AppID: "app-2"},
		},
	}

	if err := SaveAuth(input); err != nil {
		t.Fatalf("SaveAuth error: %v", err)
	}

	output, err := LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth error: %v", err)
	}

	if !reflect.DeepEqual(input, output) {
		t.Fatalf("auth mismatch: %#v != %#v", input, output)
	}
}

func TestEnsureDefault(t *testing.T) {
	auth := AuthConfig{
		Accounts: []Account{{User: "u1"}, {User: "u2"}},
	}

	changed := auth.EnsureDefault()
	if !changed {
		t.Fatalf("expected EnsureDefault to change default")
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default u1, got %s", auth.Default)
	}

	changed = auth.EnsureDefault()
	if changed {
		t.Fatalf("expected no change on second call")
	}
}

func TestDefaultAccount(t *testing.T) {
	auth := AuthConfig{
		Default:  "missing",
		Accounts: []Account{{User: "u1"}},
	}

	acct, changed, err := auth.DefaultAccount()
	if err != nil {
		t.Fatalf("DefaultAccount error: %v", err)
	}
	if !changed {
		t.Fatalf("expected default to be updated")
	}
	if acct.User != "u1" {
		t.Fatalf("unexpected user: %s", acct.User)
	}

	empty := AuthConfig{}
	_, _, err = empty.DefaultAccount()
	if err == nil {
		t.Fatalf("expected error for empty auth")
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
}

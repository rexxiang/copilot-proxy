package config

import (
	"encoding/json"
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
	if settings.MessagesInitSeqAgent {
		t.Fatalf("expected messages_init_seq_agent default false")
	}
}

func TestSaveLoadSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := Settings{
		ListenAddr:           "127.0.0.1:1234",
		UpstreamBase:         "https://example.com",
		MessagesInitSeqAgent: true,
		RequiredHeaders: map[string]string{
			"X-Test": "1",
		},
		UpstreamTimeout: NewDuration(45 * time.Second),
		MaxRetries:      5,
		RetryBackoff:    NewDuration(2 * time.Second),
	}

	if err := SaveSettings(&input); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	output, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings error: %v", err)
	}

	if !reflect.DeepEqual(input, output) {
		t.Fatalf("settings mismatch: %#v != %#v", input, output)
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

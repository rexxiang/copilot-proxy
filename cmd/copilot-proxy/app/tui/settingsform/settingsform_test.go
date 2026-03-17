package settingsform

import (
	"reflect"
	"strings"
	"testing"
	"time"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
)

func TestSettingsFieldSpecsMatrix(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	byKey := make(map[string]FieldSpec, len(specs))
	for i := range specs {
		byKey[specs[i].Key] = specs[i]
	}

	cases := []struct {
		key      string
		widget   FieldWidget
		visible  bool
		readonly bool
	}{
		{key: "listen_addr", widget: WidgetText, visible: false, readonly: true},
		{key: "upstream_base", widget: WidgetURL, visible: true, readonly: true},
		{key: "max_retries", widget: WidgetInt, visible: true, readonly: false},
		{key: "retry_backoff", widget: WidgetDuration, visible: true, readonly: false},
		{key: "rate_limit_seconds", widget: WidgetInt, visible: true, readonly: false},
		{key: "messages_agent_detection_request_mode", widget: WidgetBool, visible: true, readonly: false},
		{key: "required_headers", widget: WidgetKeyValue, visible: false, readonly: false},
		{key: "reasoning_policies", widget: WidgetKeyValue, visible: false, readonly: false},
		{key: "reasoning_policies_ui", widget: WidgetArray, visible: true, readonly: false},
		{key: "claude_haiku_fallback_models_ui", widget: WidgetArray, visible: true, readonly: false},
	}

	for _, tc := range cases {
		spec, ok := byKey[tc.key]
		if !ok {
			t.Fatalf("missing field spec for key %q", tc.key)
		}
		if spec.Widget != tc.widget {
			t.Fatalf("unexpected widget for %s: got %s want %s", tc.key, spec.Widget, tc.widget)
		}
		if spec.Visible != tc.visible {
			t.Fatalf("unexpected visible for %s: got %v want %v", tc.key, spec.Visible, tc.visible)
		}
		if spec.ReadOnly != tc.readonly {
			t.Fatalf("unexpected readonly for %s: got %v want %v", tc.key, spec.ReadOnly, tc.readonly)
		}
	}
}

func TestSettingsFieldSpecsUseReadableLabels(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	byKey := make(map[string]FieldSpec, len(specs))
	for i := range specs {
		byKey[specs[i].Key] = specs[i]
	}

	if got := byKey["messages_agent_detection_request_mode"].Label; got != "Msg Agent Mode" {
		t.Fatalf("unexpected messages_agent_detection_request_mode label: %q", got)
	}
	if got := byKey["messages_agent_detection_request_mode"].Description; got != "" {
		t.Fatalf("expected empty description, got %q", got)
	}
	if got := strings.TrimSpace(byKey["max_retries"].Description); got == "" {
		t.Fatalf("expected short description for max_retries")
	}
	if got := strings.TrimSpace(byKey["retry_backoff"].Description); got == "" {
		t.Fatalf("expected short description for retry_backoff")
	}
	if got := strings.TrimSpace(byKey["rate_limit_seconds"].Description); got == "" {
		t.Fatalf("expected short description for rate_limit_seconds")
	}
	if got := strings.TrimSpace(byKey["reasoning_policies_ui"].Description); got == "" {
		t.Fatalf("expected short description for reasoning_policies_ui")
	}
	if got := strings.TrimSpace(byKey["claude_haiku_fallback_models_ui"].Description); got == "" {
		t.Fatalf("expected short description for claude_haiku_fallback_models_ui")
	}
}

func TestEncodeDecodeSettingsForm(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	base := appsettings.Settings{
		ListenAddr:                        "127.0.0.1:4999",
		UpstreamBase:                      "https://api.githubcopilot.com",
		MessagesAgentDetectionRequestMode: false,
		RequiredHeaders: map[string]string{
			"user-agent": "copilot/1.0",
		},
		MaxRetries:       3,
		RetryBackoff:     appsettings.NewDuration(2 * time.Second),
		RateLimitSeconds: 0,
		ReasoningPolicies: []appsettings.ReasoningPolicy{
			{Model: "gpt-5-mini", Target: "responses", Effort: "low"},
		},
		ClaudeHaikuFallbackModels: []string{"gpt-5-mini", "grok-code-fast-1"},
	}
	base.SyncViewFieldsFromStorage()

	form, err := EncodeSettingsToForm(&base, specs)
	if err != nil {
		t.Fatalf("EncodeSettingsToForm error: %v", err)
	}

	form.ScalarValues["max_retries"] = "6"
	form.ScalarValues["retry_backoff"] = "5s"
	form.ScalarValues["rate_limit_seconds"] = ""
	form.ScalarValues["messages_agent_detection_request_mode"] = "true"
	form.ObjectArrayValues["reasoning_policies_ui"] = []map[string]string{
		{"model": "gpt-5-mini", "target": "responses", "effort": "high"},
		{"model": "grok-code-fast-1", "target": "chat", "effort": "none"},
	}
	form.ObjectArrayValues["claude_haiku_fallback_models_ui"] = []map[string]string{
		{"model": "grok-code-fast-1"},
		{"model": "gpt-5-mini"},
	}

	decoded, err := DecodeFormToSettings(&base, specs, form)
	if err != nil {
		t.Fatalf("DecodeFormToSettings error: %v", err)
	}

	if decoded.ListenAddr != base.ListenAddr {
		t.Fatalf("hidden listen_addr should remain unchanged: got %q want %q", decoded.ListenAddr, base.ListenAddr)
	}
	if decoded.UpstreamBase != base.UpstreamBase {
		t.Fatalf("readonly upstream_base should remain unchanged: got %q want %q", decoded.UpstreamBase, base.UpstreamBase)
	}
	if decoded.MaxRetries != 6 {
		t.Fatalf("unexpected max_retries: %d", decoded.MaxRetries)
	}
	if decoded.RetryBackoff.Duration() != 5*time.Second {
		t.Fatalf("unexpected retry_backoff: %s", decoded.RetryBackoff.Duration())
	}
	if decoded.RateLimitSeconds != 0 {
		t.Fatalf("expected empty rate_limit_seconds to decode as 0, got %d", decoded.RateLimitSeconds)
	}
	if !decoded.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode=true")
	}
	if decoded.RequiredHeaders["user-agent"] != "copilot/1.0" {
		t.Fatalf("hidden required_headers should remain unchanged")
	}
	if len(decoded.ReasoningPolicies) != 2 {
		t.Fatalf("expected two reasoning policies, got %#v", decoded.ReasoningPolicies)
	}
	if decoded.ReasoningPolicies[0].Effort != "high" || decoded.ReasoningPolicies[1].Effort != "none" {
		t.Fatalf("unexpected decoded reasoning policies: %#v", decoded.ReasoningPolicies)
	}
	if !reflect.DeepEqual(decoded.ClaudeHaikuFallbackModels, []string{"grok-code-fast-1", "gpt-5-mini"}) {
		t.Fatalf("unexpected decoded haiku fallbacks: %#v", decoded.ClaudeHaikuFallbackModels)
	}
}

func TestDecodeFormToSettingsValidation(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	base := appsettings.DefaultSettings()
	baseForm, err := EncodeSettingsToForm(&base, specs)
	if err != nil {
		t.Fatalf("EncodeSettingsToForm error: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(form *SettingsForm)
	}{
		{
			name: "invalid duration",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["retry_backoff"] = "abc"
			},
		},
		{
			name: "invalid int",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["max_retries"] = "abc"
			},
		},
		{
			name: "invalid bool",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["messages_agent_detection_request_mode"] = "maybe"
			},
		},
		{
			name: "invalid rate limit int",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["rate_limit_seconds"] = "abc"
			},
		},
		{
			name: "negative rate limit int",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["rate_limit_seconds"] = "-1"
			},
		},
		{
			name: "invalid url",
			mutate: func(form *SettingsForm) {
				form.ScalarValues["upstream_base"] = "http://localhost"
			},
		},
		{
			name: "invalid array enum effort",
			mutate: func(form *SettingsForm) {
				form.ObjectArrayValues["reasoning_policies_ui"] = []map[string]string{
					{"model": "gpt-5-mini", "target": "responses", "effort": "max"},
				}
			},
		},
	}

	for _, tc := range tests {
		form := baseForm.Clone()
		tc.mutate(&form)
		if _, err := DecodeFormToSettings(&base, specs, form); err == nil {
			t.Fatalf("%s: expected validation error", tc.name)
		}
	}
}

func TestEncodeDecodeMapRowsSupportsObjectValue(t *testing.T) {
	type mapValue struct {
		Enabled bool   `json:"enabled"`
		Level   string `json:"level"`
	}
	input := map[string]mapValue{
		"rule-a": {Enabled: true, Level: "high"},
	}

	rows, err := encodeMapRows(reflect.ValueOf(input))
	if err != nil {
		t.Fatalf("encodeMapRows error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	if !strings.Contains(rows[0].Value, "\"enabled\":true") {
		t.Fatalf("expected json value encoding, got %q", rows[0].Value)
	}

	decoded, err := decodeMapRows(rows, reflect.TypeOf(map[string]mapValue{}))
	if err != nil {
		t.Fatalf("decodeMapRows error: %v", err)
	}
	got, ok := decoded.Interface().(map[string]mapValue)
	if !ok {
		t.Fatalf("expected map[string]mapValue, got %T", decoded.Interface())
	}
	if !reflect.DeepEqual(input, got) {
		t.Fatalf("decoded map mismatch: %#v != %#v", input, got)
	}
}

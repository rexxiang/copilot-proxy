package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSettingsFieldSpecs_Matrix(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	byKey := make(map[string]FieldSpec, len(specs))
	for i := range specs {
		spec := specs[i]
		byKey[spec.Key] = spec
	}

	cases := []struct {
		key      string
		widget   FieldWidget
		visible  bool
		readonly bool
	}{
		{key: "listen_addr", widget: WidgetText, visible: false, readonly: true},
		{key: "upstream_base", widget: WidgetURL, visible: true, readonly: true},
		{key: "upstream_timeout", widget: WidgetDuration, visible: true, readonly: false},
		{key: "max_retries", widget: WidgetInt, visible: true, readonly: false},
		{key: "retry_backoff", widget: WidgetDuration, visible: true, readonly: false},
		{key: "messages_init_seq_agent", widget: WidgetBool, visible: true, readonly: false},
		{key: "required_headers", widget: WidgetKeyValue, visible: false, readonly: false},
		{key: "reasoning_policies", widget: WidgetKeyValue, visible: false, readonly: false},
		{key: "reasoning_policies_ui", widget: WidgetArray, visible: true, readonly: false},
	}

	for i := range cases {
		tc := cases[i]
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

func TestBuildFieldSpecsForType_DefaultVisibilityReadonly(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}

	specs, err := buildFieldSpecsForType(reflect.TypeOf(sample{}))
	if err != nil {
		t.Fatalf("buildFieldSpecsForType error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one spec, got %d", len(specs))
	}
	if specs[0].Visible {
		t.Fatalf("expected default visible=false")
	}
	if !specs[0].ReadOnly {
		t.Fatalf("expected default readonly=true")
	}
}

func TestBuildFieldSpecsForType_UnknownTagKey(t *testing.T) {
	type invalid struct {
		Name string `json:"name" ui:"visible=true;badkey=1"`
	}

	_, err := buildFieldSpecsForType(reflect.TypeOf(invalid{}))
	if err == nil {
		t.Fatalf("expected error for unknown ui tag key")
	}
	if !strings.Contains(err.Error(), "unknown ui key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildFieldSpecsForType_LabelTag(t *testing.T) {
	type sample struct {
		Name string `json:"name" ui:"label=Display Name;visible=true;readonly=false"`
	}

	specs, err := buildFieldSpecsForType(reflect.TypeOf(sample{}))
	if err != nil {
		t.Fatalf("buildFieldSpecsForType error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one spec, got %d", len(specs))
	}
	if specs[0].Label != "Display Name" {
		t.Fatalf("unexpected label mapping: got %q", specs[0].Label)
	}
}

func TestBuildFieldSpecsForType_EnumAndDescription(t *testing.T) {
	type sample struct {
		Mode string `json:"mode" ui:"label=Mode;visible=true;readonly=false;enum=a,b,'c';description=mode field"`
	}

	specs, err := buildFieldSpecsForType(reflect.TypeOf(sample{}))
	if err != nil {
		t.Fatalf("buildFieldSpecsForType error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one spec, got %d", len(specs))
	}
	if specs[0].Description != "mode field" {
		t.Fatalf("unexpected description: %q", specs[0].Description)
	}
	if !reflect.DeepEqual(specs[0].EnumValues, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected enum values: %#v", specs[0].EnumValues)
	}
}

func TestBuildFieldSpecsForType_UIOnlyFieldUsesTagKey(t *testing.T) {
	type sample struct {
		Hidden string `json:"-" ui:"key=hidden_key;label=Hidden;widget=text;visible=true;readonly=false"`
	}

	specs, err := buildFieldSpecsForType(reflect.TypeOf(sample{}))
	if err != nil {
		t.Fatalf("buildFieldSpecsForType error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected one spec, got %d", len(specs))
	}
	if specs[0].Key != "hidden_key" {
		t.Fatalf("expected hidden_key, got %q", specs[0].Key)
	}
}

func TestEncodeDecodeSettingsForm(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	base := Settings{
		ListenAddr:           "127.0.0.1:4999",
		UpstreamBase:         "https://api.githubcopilot.com",
		MessagesInitSeqAgent: false,
		RequiredHeaders: map[string]string{
			"user-agent": "copilot/1.0",
		},
		UpstreamTimeout: NewDuration(40 * time.Second),
		MaxRetries:      3,
		RetryBackoff:    NewDuration(2 * time.Second),
		ReasoningPolicies: []ReasoningPolicy{
			{Model: "gpt-5-mini", Target: "responses", Effort: "low"},
		},
	}

	form, err := EncodeSettingsToForm(&base, specs)
	if err != nil {
		t.Fatalf("EncodeSettingsToForm error: %v", err)
	}

	form.ScalarValues["upstream_timeout"] = "1m0s"
	form.ScalarValues["max_retries"] = "6"
	form.ScalarValues["retry_backoff"] = "5s"
	form.ScalarValues["messages_init_seq_agent"] = "true"
	form.ObjectArrayValues["reasoning_policies_ui"] = []map[string]string{
		{"model": "gpt-5-mini", "target": "responses", "effort": "high"},
		{"model": "grok-code-fast-1", "target": "chat", "effort": "none"},
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
	if decoded.UpstreamTimeout.Duration() != time.Minute {
		t.Fatalf("unexpected upstream_timeout: %s", decoded.UpstreamTimeout.Duration())
	}
	if decoded.MaxRetries != 6 {
		t.Fatalf("unexpected max_retries: %d", decoded.MaxRetries)
	}
	if decoded.RetryBackoff.Duration() != 5*time.Second {
		t.Fatalf("unexpected retry_backoff: %s", decoded.RetryBackoff.Duration())
	}
	if !decoded.MessagesInitSeqAgent {
		t.Fatalf("expected messages_init_seq_agent=true")
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
}

func TestDecodeFormToSettings_Validation(t *testing.T) {
	specs, err := SettingsFieldSpecs()
	if err != nil {
		t.Fatalf("SettingsFieldSpecs error: %v", err)
	}

	base := DefaultSettings()

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
				form.ScalarValues["upstream_timeout"] = "abc"
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
				form.ScalarValues["messages_init_seq_agent"] = "maybe"
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

	for i := range tests {
		tc := tests[i]
		form := baseForm.Clone()
		tc.mutate(&form)

		if _, err := DecodeFormToSettings(&base, specs, form); err == nil {
			t.Fatalf("%s: expected validation error", tc.name)
		}
	}
}

func TestBuildFieldSpecsForType_VisibleMapFieldUnsupported(t *testing.T) {
	type invalid struct {
		Headers map[string]string `json:"headers" ui:"label=Headers;widget=kv;visible=true;readonly=false"`
	}

	_, err := buildFieldSpecsForType(reflect.TypeOf(invalid{}))
	if err == nil {
		t.Fatalf("expected error for visible map field")
	}
	if !strings.Contains(err.Error(), "visible map field unsupported") {
		t.Fatalf("unexpected error: %v", err)
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

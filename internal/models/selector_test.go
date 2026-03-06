package models

import "testing"

const modelGPT5Mini = "gpt-5-mini"

func TestSelectExactCaseInsensitive(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "gpt-4o", Family: "gpt-4o"},
		{ID: "claude-sonnet-4.5", Family: "claude-sonnet-4.5"},
	}

	selected, ok := selector.Select(models, "GPT-4O")
	if ok || selected != "gpt-4o" {
		t.Fatalf("expected no rewrite on exact match, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedClaudeSonnet(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "claude-sonnet-4.1", Family: "claude-sonnet-4.1"},
		{ID: "claude-sonnet-4.10", Family: "claude-sonnet-4.10"},
		{ID: "claude-sonnet-4.5", Family: "claude-sonnet-4.5"},
	}

	selected, ok := selector.Select(models, "claude-sonnet-4")
	if !ok || selected != "claude-sonnet-4.10" {
		t.Fatalf("expected highest sonnet version, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedHaikuFallback(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: modelGPT5Mini, Family: modelGPT5Mini},
		{ID: "claude-haiku-3.1", Family: "claude-haiku-3.1"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != modelGPT5Mini {
		t.Fatalf("expected haiku fallback to %s, got %q (ok=%v)", modelGPT5Mini, selected, ok)
	}
}

func TestSelectPrefersExactClaudeHaikuOverMappedFallback(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: modelGPT5Mini, Family: modelGPT5Mini},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, mapped := selector.Select(models, "CLAUDE-HAIKU-3.2")
	if mapped {
		t.Fatalf("expected exact match to avoid mapped fallback")
	}
	if selected != "claude-haiku-3.2" {
		t.Fatalf("expected exact haiku model, got %q", selected)
	}
}

func TestSelectMappedHaikuPrefersGPT5Mini(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: modelGPT5Mini, Family: modelGPT5Mini},
		{ID: "grok-code-fast-1", Family: "grok-code-fast-1"},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != modelGPT5Mini {
		t.Fatalf("expected haiku fallback to %s before grok-code-fast-1, got %q (ok=%v)", modelGPT5Mini, selected, ok)
	}
}

func TestSelectMappedHaikuFallsBackToGrokWhenGPT5MiniMissing(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "grok-code-fast-1", Family: "grok-code-fast-1"},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != "grok-code-fast-1" {
		t.Fatalf("expected haiku fallback to grok-code-fast-1 when gpt-5-mini is missing, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedHaikuUsesConfiguredFallbackOrder(t *testing.T) {
	selector := NewSelectorWithConfig(SelectorConfig{
		ClaudeHaikuFallbackModels: []string{"grok-code-fast-1", modelGPT5Mini},
	})
	models := []ModelInfo{
		{ID: modelGPT5Mini, Family: modelGPT5Mini},
		{ID: "grok-code-fast-1", Family: "grok-code-fast-1"},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != "grok-code-fast-1" {
		t.Fatalf("expected configured haiku fallback order to prefer grok-code-fast-1, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedHaikuWithEmptyConfiguredFallbacksUsesBuiltInHaikuFallback(t *testing.T) {
	selector := NewSelectorWithConfig(SelectorConfig{
		ClaudeHaikuFallbackModels: []string{},
	})
	models := []ModelInfo{
		{ID: modelGPT5Mini, Family: modelGPT5Mini},
		{ID: "grok-code-fast-1", Family: "grok-code-fast-1"},
		{ID: "claude-haiku-3.10", Family: "claude-haiku-3.10"},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != "claude-haiku-3.10" {
		t.Fatalf("expected empty configured fallbacks to use built-in haiku fallback, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedHaikuHighestVersion(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "claude-haiku-3.1", Family: "claude-haiku-3.1"},
		{ID: "claude-haiku-3.10", Family: "claude-haiku-3.10"},
		{ID: "claude-haiku-3.2", Family: "claude-haiku-3.2"},
	}

	selected, ok := selector.Select(models, "claude-haiku-3")
	if !ok || selected != "claude-haiku-3.10" {
		t.Fatalf("expected highest haiku version, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedClaudeOpus(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "claude-opus-3.0", Family: "claude-opus-3.0"},
		{ID: "claude-opus-3.1", Family: "claude-opus-3.1"},
	}

	selected, ok := selector.Select(models, "claude-opus-3")
	if !ok || selected != "claude-opus-3.1" {
		t.Fatalf("expected highest opus version, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectMappedClaudeOtherPrefixNoMatch(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "claude-opus-3.1", Family: "claude-opus-3.1"},
	}

	selected, ok := selector.Select(models, "claude-instant-1")
	if ok || selected != "" {
		t.Fatalf("expected no match for other claude prefix, got %q (ok=%v)", selected, ok)
	}
}

func TestSelectNoMatchNoRewrite(t *testing.T) {
	selector := NewSelector()
	models := []ModelInfo{
		{ID: "gpt-4o", Family: "gpt-4o"},
	}

	selected, ok := selector.Select(models, "unknown-model")
	if ok || selected != "" {
		t.Fatalf("expected no match, got %q (ok=%v)", selected, ok)
	}
}

func TestParseVersionSegmentsEdgeCases(t *testing.T) {
	a := parseVersionSegments("4.10")
	b := parseVersionSegments("4.5")
	if compareSegments(a, b) <= 0 {
		t.Fatalf("expected 4.10 > 4.5")
	}

	c := parseVersionSegments("")
	d := parseVersionSegments("0")
	if compareSegments(c, d) != 0 {
		t.Fatalf("expected empty to compare equal to 0")
	}

	e := parseVersionSegments("3.1-beta2")
	f := parseVersionSegments("3.1")
	if compareSegments(e, f) != 0 {
		t.Fatalf("expected 3.1-beta2 to compare equal to 3.1")
	}
}

func TestSelectModelInfoReturnsMappedModelWithEndpoints(t *testing.T) {
	selector := NewSelector()
	items := []ModelInfo{
		{ID: "gpt-5-mini", Endpoints: []string{"/responses", "/chat/completions"}},
		{ID: "grok-code-fast-1", Endpoints: []string{"/responses"}},
	}

	model, mapped, found := selector.SelectModelInfo(items, "claude-haiku-3")
	if !found {
		t.Fatalf("expected model to be found")
	}
	if !mapped {
		t.Fatalf("expected mapped=true for haiku fallback")
	}
	if model.ID != "gpt-5-mini" {
		t.Fatalf("expected mapped model id, got %q", model.ID)
	}
	if len(model.Endpoints) != 2 {
		t.Fatalf("expected endpoints to be preserved, got %v", model.Endpoints)
	}
}

func TestSelectModelInfoPrefersExactOverMappedFallback(t *testing.T) {
	selector := NewSelector()
	items := []ModelInfo{
		{ID: modelGPT5Mini, Endpoints: []string{"/responses"}},
		{ID: "claude-haiku-3.2", Endpoints: []string{"/v1/messages"}},
	}

	model, mapped, found := selector.SelectModelInfo(items, "CLAUDE-HAIKU-3.2")
	if !found {
		t.Fatalf("expected model to be found")
	}
	if mapped {
		t.Fatalf("expected exact model selection to report mapped=false")
	}
	if model.ID != "claude-haiku-3.2" {
		t.Fatalf("expected exact haiku model, got %q", model.ID)
	}
	if len(model.Endpoints) != 1 || model.Endpoints[0] != "/v1/messages" {
		t.Fatalf("expected endpoints from exact model, got %v", model.Endpoints)
	}
}

func TestSelectModelInfoReturnsExactModel(t *testing.T) {
	selector := NewSelector()
	items := []ModelInfo{
		{ID: "gpt-4o", Endpoints: []string{"/chat/completions"}},
	}

	model, mapped, found := selector.SelectModelInfo(items, "gpt-4o")
	if !found {
		t.Fatalf("expected exact model to be found")
	}
	if mapped {
		t.Fatalf("expected mapped=false for exact match")
	}
	if model.ID != "gpt-4o" {
		t.Fatalf("expected exact model, got %q", model.ID)
	}
}

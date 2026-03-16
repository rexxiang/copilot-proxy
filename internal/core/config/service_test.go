package coreconfig

import (
	"testing"

	"copilot-proxy/internal/config"
)

func TestConfigServiceModelMappingsAreCopied(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	svc := NewService(config.DefaultSettings())
	if err := svc.UpdateModelMappings(map[string]string{"foo@chat": "high"}, []string{"gpt-5-mini"}); err != nil {
		t.Fatalf("update mappings: %v", err)
	}
	policies, fallbacks := svc.GetModelMappings()
	if policies == nil || fallbacks == nil {
		t.Fatalf("expected non-nil mappings")
	}

	policies["foo"] = "bar"
	fallbacks[0] = "x"

	nextPolicies, nextFallbacks := svc.GetModelMappings()
	if _, ok := nextPolicies["foo"]; ok {
		t.Fatalf("mappings mutated original state")
	}
	if nextFallbacks[0] == "x" {
		t.Fatalf("fallbacks mutated original state")
	}
}

func TestConfigServiceUpdateModelMappings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	svc := NewService(config.DefaultSettings())
	policies := map[string]string{"model@chat": "high"}
	fallbacks := []string{"gpt-5-mini"}

	if err := svc.UpdateModelMappings(policies, fallbacks); err != nil {
		t.Fatalf("update mappings: %v", err)
	}

	nextPolicies, nextFallbacks := svc.GetModelMappings()
	if nextPolicies["model@chat"] != "high" {
		t.Fatalf("expected policy stored, got %v", nextPolicies["model@chat"])
	}
	if len(nextFallbacks) != 1 || nextFallbacks[0] != "gpt-5-mini" {
		t.Fatalf("expected fallback stored, got %v", nextFallbacks)
	}
}

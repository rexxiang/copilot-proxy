package reasoning

import (
	"errors"
	"testing"
)

func TestNormalizeClientEffort(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "xhigh", want: EffortHigh},
		{raw: "MAX", want: EffortHigh},
		{raw: "high", want: EffortHigh},
		{raw: "Medium", want: EffortMedium},
		{raw: "LOW", want: EffortLow},
		{raw: "minimal", want: EffortLow},
		{raw: "ultra", want: EffortNone},
		{raw: "", want: EffortNone},
	}
	for _, tc := range cases {
		if got := NormalizeClientEffort(tc.raw); got != tc.want {
			t.Fatalf("NormalizeClientEffort(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestResolveMappedEffort(t *testing.T) {
	got, ok := ResolveMappedEffort(EffortHigh, nil)
	if ok || got != "" {
		t.Fatalf("expected no effort when model support is empty, got %q ok=%v", got, ok)
	}

	got, ok = ResolveMappedEffort(EffortHigh, []string{EffortLow})
	if !ok || got != EffortLow {
		t.Fatalf("expected closest fallback to low, got %q ok=%v", got, ok)
	}

	got, ok = ResolveMappedEffort(EffortNone, []string{EffortLow, EffortHigh})
	if ok || got != "" {
		t.Fatalf("expected none to skip upstream field, got %q ok=%v", got, ok)
	}
}

func TestParsePolicyMapAndBuildPolicyMap(t *testing.T) {
	parsed, err := ParsePolicyMap(map[string]string{
		"gpt-5-mini@responses":  "medium",
		"grok-code-fast-1@chat": "none",
	})
	if err != nil {
		t.Fatalf("ParsePolicyMap error: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(parsed))
	}

	encoded, err := BuildPolicyMap(parsed)
	if err != nil {
		t.Fatalf("BuildPolicyMap error: %v", err)
	}
	if encoded["gpt-5-mini@responses"] != "medium" {
		t.Fatalf("expected encoded responses policy, got %#v", encoded)
	}
	if encoded["grok-code-fast-1@chat"] != "none" {
		t.Fatalf("expected encoded chat policy, got %#v", encoded)
	}
}

func TestParsePolicyMapRejectsInvalidEntries(t *testing.T) {
	_, err := ParsePolicyMap(map[string]string{
		"bad-key": "high",
	})
	if err == nil || !errors.Is(err, ErrInvalidPolicyKey) {
		t.Fatalf("expected invalid policy key error, got %v", err)
	}

	_, err = ParsePolicyMap(map[string]string{
		"gpt-5-mini@responses": "max",
	})
	if err == nil || !errors.Is(err, ErrInvalidPolicyEffort) {
		t.Fatalf("expected invalid policy effort error, got %v", err)
	}
}

func TestMatchPolicy(t *testing.T) {
	policies := []Policy{
		{Model: "gpt-5-mini", Target: TargetResponses, Effort: EffortLow},
		{Model: "*", Target: TargetChat, Effort: EffortNone},
	}

	got, ok := MatchPolicy(policies, "GPT-5-MINI", TargetResponses)
	if !ok || got != EffortLow {
		t.Fatalf("expected exact model match low, got %q ok=%v", got, ok)
	}

	got, ok = MatchPolicy(policies, "unknown", TargetChat)
	if !ok || got != EffortNone {
		t.Fatalf("expected wildcard policy none, got %q ok=%v", got, ok)
	}
}

func TestEffectivePoliciesFromMap_UsesBuiltinWhenUnset(t *testing.T) {
	policies, err := EffectivePoliciesFromMap(nil)
	if err != nil {
		t.Fatalf("EffectivePoliciesFromMap error: %v", err)
	}

	got, ok := MatchPolicy(policies, "gpt-5-mini", TargetResponses)
	if !ok || got != EffortNone {
		t.Fatalf("expected builtin gpt-5-mini@responses=none, got %q ok=%v", got, ok)
	}
	got, ok = MatchPolicy(policies, "grok-code-fast-1", TargetChat)
	if !ok || got != EffortNone {
		t.Fatalf("expected builtin grok-code-fast-1@chat=none, got %q ok=%v", got, ok)
	}
}

func TestEffectivePoliciesFromMap_ConfigOverridesBuiltin(t *testing.T) {
	policies, err := EffectivePoliciesFromMap(map[string]string{
		"gpt-5-mini@responses": "high",
	})
	if err != nil {
		t.Fatalf("EffectivePoliciesFromMap error: %v", err)
	}

	got, ok := MatchPolicy(policies, "gpt-5-mini", TargetResponses)
	if !ok || got != EffortHigh {
		t.Fatalf("expected override gpt-5-mini@responses=high, got %q ok=%v", got, ok)
	}
}

func TestEffectivePoliciesFromMap_PreservesNonBuiltinPolicies(t *testing.T) {
	policies, err := EffectivePoliciesFromMap(map[string]string{
		"gpt-4o@responses": "medium",
	})
	if err != nil {
		t.Fatalf("EffectivePoliciesFromMap error: %v", err)
	}

	got, ok := MatchPolicy(policies, "gpt-4o", TargetResponses)
	if !ok || got != EffortMedium {
		t.Fatalf("expected custom gpt-4o@responses=medium, got %q ok=%v", got, ok)
	}
}

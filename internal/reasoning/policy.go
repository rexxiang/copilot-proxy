package reasoning

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	EffortNone   = "none"
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"

	TargetChat      = "chat"
	TargetResponses = "responses"
)

var (
	ErrInvalidPolicyKey    = errors.New("invalid reasoning policy key")
	ErrInvalidPolicyTarget = errors.New("invalid reasoning policy target")
	ErrInvalidPolicyEffort = errors.New("invalid reasoning policy effort")
	ErrDuplicatePolicy     = errors.New("duplicate reasoning policy")
)

var effortRank = map[string]int{
	EffortLow:    1,
	EffortMedium: 2,
	EffortHigh:   3,
}

var builtinPolicyMap = map[string]string{
	"gpt-5-mini@responses":  EffortNone,
	"grok-code-fast-1@chat": EffortNone,
}

// Policy binds model+target to a configured effort.
type Policy struct {
	Model  string
	Target string
	Effort string
}

// BuiltinPolicyMap returns default reasoning policies used at runtime.
func BuiltinPolicyMap() map[string]string {
	clone := make(map[string]string, len(builtinPolicyMap))
	for key, value := range builtinPolicyMap {
		clone[key] = value
	}
	return clone
}

// EffectivePoliciesFromMap merges built-in defaults with user overrides.
func EffectivePoliciesFromMap(overrides map[string]string) ([]Policy, error) {
	merged := BuiltinPolicyMap()
	for key, value := range overrides {
		merged[key] = value
	}
	return ParsePolicyMap(merged)
}

// NormalizeClientEffort converts client effort values into internal levels.
func NormalizeClientEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "xhigh", "max", EffortHigh:
		return EffortHigh
	case EffortMedium:
		return EffortMedium
	case EffortLow, "minimal":
		return EffortLow
	default:
		return EffortNone
	}
}

// NormalizePolicyEffort validates configured effort values.
func NormalizePolicyEffort(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case EffortNone:
		return EffortNone, true
	case EffortLow:
		return EffortLow, true
	case EffortMedium:
		return EffortMedium, true
	case EffortHigh:
		return EffortHigh, true
	default:
		return "", false
	}
}

// NormalizeSupportedEfforts filters and deduplicates model-supported effort levels.
func NormalizeSupportedEfforts(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		effort, ok := normalizeSupportedEffort(raw)
		if !ok {
			continue
		}
		if _, exists := seen[effort]; exists {
			continue
		}
		seen[effort] = struct{}{}
		out = append(out, effort)
	}
	sort.Slice(out, func(i, j int) bool {
		return effortRank[out[i]] < effortRank[out[j]]
	})
	return out
}

func normalizeSupportedEffort(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case EffortLow:
		return EffortLow, true
	case EffortMedium:
		return EffortMedium, true
	case EffortHigh:
		return EffortHigh, true
	default:
		return "", false
	}
}

// ParsePolicyMap decodes settings map entries (<model>@<target>=<effort>).
func ParsePolicyMap(raw map[string]string) ([]Policy, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	seen := make(map[string]struct{}, len(raw))
	out := make([]Policy, 0, len(raw))
	for _, key := range keys {
		model, target, err := parsePolicyKey(key)
		if err != nil {
			return nil, err
		}
		effort, ok := NormalizePolicyEffort(raw[key])
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPolicyEffort, raw[key])
		}
		normalizedID := strings.ToLower(model) + "@" + target
		if _, exists := seen[normalizedID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicatePolicy, key)
		}
		seen[normalizedID] = struct{}{}
		out = append(out, Policy{Model: model, Target: target, Effort: effort})
	}
	return out, nil
}

// BuildPolicyMap encodes policy entries into settings map shape.
func BuildPolicyMap(policies []Policy) (map[string]string, error) {
	if len(policies) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(policies))
	result := make(map[string]string, len(policies))
	for _, item := range policies {
		model := strings.TrimSpace(item.Model)
		if model == "" {
			return nil, fmt.Errorf("%w: empty model", ErrInvalidPolicyKey)
		}
		target, ok := normalizeTarget(item.Target)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPolicyTarget, item.Target)
		}
		effort, ok := NormalizePolicyEffort(item.Effort)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPolicyEffort, item.Effort)
		}

		normalizedID := strings.ToLower(model) + "@" + target
		if _, exists := seen[normalizedID]; exists {
			return nil, fmt.Errorf("%w: %s@%s", ErrDuplicatePolicy, model, target)
		}
		seen[normalizedID] = struct{}{}
		result[model+"@"+target] = effort
	}
	return result, nil
}

// MatchPolicy returns the best matching configured effort for model+target.
func MatchPolicy(policies []Policy, model, target string) (string, bool) {
	if len(policies) == 0 {
		return "", false
	}
	normalizedTarget, ok := normalizeTarget(target)
	if !ok {
		return "", false
	}
	trimmedModel := strings.TrimSpace(model)

	for _, item := range policies {
		if item.Target != normalizedTarget {
			continue
		}
		if strings.EqualFold(item.Model, trimmedModel) {
			return item.Effort, true
		}
	}
	for _, item := range policies {
		if item.Target != normalizedTarget {
			continue
		}
		if item.Model == "*" {
			return item.Effort, true
		}
	}
	return "", false
}

// ResolveMappedEffort resolves candidate effort against model-supported levels.
// The boolean return indicates whether a reasoning field should be sent upstream.
func ResolveMappedEffort(candidate string, supported []string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(candidate), EffortNone) {
		return "", false
	}

	normalizedCandidate, ok := normalizeSupportedEffort(candidate)
	if !ok {
		return "", false
	}
	allowed := NormalizeSupportedEfforts(supported)
	if len(allowed) == 0 {
		return "", false
	}
	for _, item := range allowed {
		if item == normalizedCandidate {
			return item, true
		}
	}

	best := ""
	bestDiff := 1 << 30
	bestRank := 1 << 30
	candidateRank := effortRank[normalizedCandidate]
	for _, item := range allowed {
		rank := effortRank[item]
		diff := abs(candidateRank - rank)
		if diff < bestDiff {
			best = item
			bestDiff = diff
			bestRank = rank
			continue
		}
		if diff == bestDiff && rank < bestRank {
			best = item
			bestRank = rank
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

func parsePolicyKey(key string) (string, string, error) {
	trimmed := strings.TrimSpace(key)
	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at >= len(trimmed)-1 {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidPolicyKey, key)
	}
	model := strings.TrimSpace(trimmed[:at])
	targetRaw := strings.TrimSpace(trimmed[at+1:])
	if model == "" {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidPolicyKey, key)
	}
	target, ok := normalizeTarget(targetRaw)
	if !ok {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidPolicyTarget, targetRaw)
	}
	return model, target, nil
}

func normalizeTarget(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case TargetChat:
		return TargetChat, true
	case TargetResponses:
		return TargetResponses, true
	default:
		return "", false
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

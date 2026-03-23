package server

import (
	"copilot-proxy/internal/runtime/reasoning"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

var (
	errInvalidMaxRetries      = errors.New("max_retries must be greater than 0")
	errInvalidRetryBackoff    = errors.New("retry_backoff must be greater than 0")
	errInvalidRateLimitSecond = errors.New("rate_limit_seconds must be >= 0")
)

// Snapshot captures the runtime-derived settings.
type Snapshot struct {
	MaxRetries                        int
	RetryBackoff                      time.Duration
	RateLimitCooldown                 time.Duration
	MessagesAgentDetectionRequestMode bool
	ClaudeHaikuFallbackModels         []string
	ReasoningPolicies                 []reasoning.Policy
}

func compileRuntimeSettingsSnapshot(settings runtimeconfig.RuntimeSettings) (Snapshot, error) {
	normalized := cloneRuntimeSettings(settings)
	defaults := runtimeconfig.Default()

	if normalized.MaxRetries <= 0 {
		return Snapshot{}, errInvalidMaxRetries
	}
	retryBackoff := normalized.RetryBackoff.Duration()
	if retryBackoff <= 0 {
		return Snapshot{}, errInvalidRetryBackoff
	}
	if normalized.RateLimitSeconds < 0 {
		return Snapshot{}, errInvalidRateLimitSecond
	}

	fallbackModels := normalizedStringSlice(normalized.ClaudeHaikuFallbackModels)
	if fallbackModels == nil {
		fallbackModels = normalizedStringSlice(defaults.ClaudeHaikuFallbackModels)
	}

	policies, err := reasoning.EffectivePoliciesFromMap(normalized.ReasoningPoliciesMap)
	if err != nil {
		return Snapshot{}, fmt.Errorf("compile reasoning policies: %w", err)
	}

	return Snapshot{
		MaxRetries:                        normalized.MaxRetries,
		RetryBackoff:                      retryBackoff,
		RateLimitCooldown:                 time.Duration(normalized.RateLimitSeconds) * time.Second,
		MessagesAgentDetectionRequestMode: normalized.MessagesAgentDetectionRequestMode,
		ClaudeHaikuFallbackModels:         cloneStringSliceRuntime(fallbackModels),
		ReasoningPolicies:                 cloneReasoningPolicies(policies),
	}, nil
}

// CompileSnapshot validates settings and returns the derived runtime snapshot.
func CompileSnapshot(settings runtimeconfig.RuntimeSettings) (Snapshot, error) {
	return compileRuntimeSettingsSnapshot(settings)
}

func cloneRuntimeSettings(input runtimeconfig.RuntimeSettings) runtimeconfig.RuntimeSettings {
	clone := input
	clone.RequiredHeaders = cloneStringMap(input.RequiredHeaders)
	clone.ReasoningPoliciesMap = cloneStringMap(input.ReasoningPoliciesMap)
	clone.ClaudeHaikuFallbackModels = cloneStringSliceRuntime(input.ClaudeHaikuFallbackModels)
	return clone
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneReasoningPolicies(input []reasoning.Policy) []reasoning.Policy {
	if input == nil {
		return nil
	}
	out := make([]reasoning.Policy, len(input))
	copy(out, input)
	return out
}

func cloneStringSliceRuntime(input []string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}

func normalizedStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, item := range input {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

package cli

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/reasoning"
)

var (
	errInvalidMaxRetries      = errors.New("max_retries must be greater than 0")
	errInvalidRetryBackoff    = errors.New("retry_backoff must be greater than 0")
	errInvalidRateLimitSecond = errors.New("rate_limit_seconds must be >= 0")
)

type runtimeSettingsSnapshot struct {
	MaxRetries                        int
	RetryBackoff                      time.Duration
	RateLimitCooldown                 time.Duration
	MessagesAgentDetectionRequestMode bool
	ClaudeHaikuFallbackModels         []string
	ReasoningPolicies                 []reasoning.Policy
}

type runtimeSettingsStore struct {
	settings atomic.Value // config.Settings
	snapshot atomic.Value // runtimeSettingsSnapshot
}

func newRuntimeSettingsStore(initial config.Settings) (*runtimeSettingsStore, error) {
	snapshot, err := compileRuntimeSettingsSnapshot(initial)
	if err != nil {
		return nil, err
	}

	store := &runtimeSettingsStore{}
	store.settings.Store(cloneRuntimeSettings(initial))
	store.snapshot.Store(cloneRuntimeSettingsSnapshot(snapshot))
	return store, nil
}

func (s *runtimeSettingsStore) Current() config.Settings {
	if s == nil {
		return config.DefaultSettings()
	}
	current, ok := s.settings.Load().(config.Settings)
	if !ok {
		return config.DefaultSettings()
	}
	return cloneRuntimeSettings(current)
}

func (s *runtimeSettingsStore) Snapshot() runtimeSettingsSnapshot {
	if s == nil {
		snapshot, _ := compileRuntimeSettingsSnapshot(config.DefaultSettings())
		return snapshot
	}
	current, ok := s.snapshot.Load().(runtimeSettingsSnapshot)
	if !ok {
		snapshot, _ := compileRuntimeSettingsSnapshot(config.DefaultSettings())
		return snapshot
	}
	return cloneRuntimeSettingsSnapshot(current)
}

func (s *runtimeSettingsStore) Validate(next config.Settings) (runtimeSettingsSnapshot, error) {
	return compileRuntimeSettingsSnapshot(next)
}

func (s *runtimeSettingsStore) Publish(next config.Settings, snapshot runtimeSettingsSnapshot) {
	if s == nil {
		return
	}
	s.settings.Store(cloneRuntimeSettings(next))
	s.snapshot.Store(cloneRuntimeSettingsSnapshot(snapshot))
}

func compileRuntimeSettingsSnapshot(settings config.Settings) (runtimeSettingsSnapshot, error) {
	normalized := cloneRuntimeSettings(settings)
	defaults := config.DefaultSettings()

	if normalized.MaxRetries <= 0 {
		return runtimeSettingsSnapshot{}, errInvalidMaxRetries
	}
	retryBackoff := normalized.RetryBackoff.Duration()
	if retryBackoff <= 0 {
		return runtimeSettingsSnapshot{}, errInvalidRetryBackoff
	}
	if normalized.RateLimitSeconds < 0 {
		return runtimeSettingsSnapshot{}, errInvalidRateLimitSecond
	}

	fallbackModels := normalizedStringSlice(normalized.ClaudeHaikuFallbackModels)
	if fallbackModels == nil {
		fallbackModels = normalizedStringSlice(defaults.ClaudeHaikuFallbackModels)
	}

	policies, err := reasoning.EffectivePoliciesFromMap(normalized.ReasoningPoliciesMap)
	if err != nil {
		return runtimeSettingsSnapshot{}, fmt.Errorf("compile reasoning policies: %w", err)
	}

	return runtimeSettingsSnapshot{
		MaxRetries:                        normalized.MaxRetries,
		RetryBackoff:                      retryBackoff,
		RateLimitCooldown:                 time.Duration(normalized.RateLimitSeconds) * time.Second,
		MessagesAgentDetectionRequestMode: normalized.MessagesAgentDetectionRequestMode,
		ClaudeHaikuFallbackModels:         cloneStringSliceRuntime(fallbackModels),
		ReasoningPolicies:                 cloneReasoningPolicies(policies),
	}, nil
}

func cloneRuntimeSettings(input config.Settings) config.Settings {
	clone := input
	clone.RequiredHeaders = cloneStringMap(input.RequiredHeaders)
	clone.ReasoningPoliciesMap = cloneStringMap(input.ReasoningPoliciesMap)
	clone.ReasoningPolicies = cloneReasoningPolicyRows(input.ReasoningPolicies)
	clone.ClaudeHaikuFallbackModels = cloneStringSliceRuntime(input.ClaudeHaikuFallbackModels)
	clone.ClaudeHaikuFallbackModelsUI = cloneHaikuFallbackUIRows(input.ClaudeHaikuFallbackModelsUI)
	return clone
}

func cloneRuntimeSettingsSnapshot(input runtimeSettingsSnapshot) runtimeSettingsSnapshot {
	clone := input
	clone.ClaudeHaikuFallbackModels = cloneStringSliceRuntime(input.ClaudeHaikuFallbackModels)
	clone.ReasoningPolicies = cloneReasoningPolicies(input.ReasoningPolicies)
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

func cloneReasoningPolicyRows(input []config.ReasoningPolicy) []config.ReasoningPolicy {
	if input == nil {
		return nil
	}
	out := make([]config.ReasoningPolicy, len(input))
	copy(out, input)
	return out
}

func cloneHaikuFallbackUIRows(input []config.HaikuFallbackModel) []config.HaikuFallbackModel {
	if input == nil {
		return nil
	}
	out := make([]config.HaikuFallbackModel, len(input))
	copy(out, input)
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

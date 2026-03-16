package runtime

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

// Snapshot captures the runtime-derived settings.
type Snapshot struct {
	MaxRetries                        int
	RetryBackoff                      time.Duration
	RateLimitCooldown                 time.Duration
	MessagesAgentDetectionRequestMode bool
	ClaudeHaikuFallbackModels         []string
	ReasoningPolicies                 []reasoning.Policy
}

// SettingsStore maintains a live copy of settings and its compiled runtime snapshot.
type SettingsStore struct {
	settings atomic.Value // config.Settings
	snapshot atomic.Value // Snapshot
}

// NewSettingsStore creates a store backed by an initial settings snapshot.
func NewSettingsStore(initial config.Settings) (*SettingsStore, error) {
	snapshot, err := compileRuntimeSettingsSnapshot(initial)
	if err != nil {
		return nil, err
	}

	store := &SettingsStore{}
	store.settings.Store(cloneRuntimeSettings(initial))
	store.snapshot.Store(cloneRuntimeSettingsSnapshot(snapshot))
	return store, nil
}

// Current returns a cloned copy of the active settings.
func (s *SettingsStore) Current() config.Settings {
	if s == nil {
		return config.DefaultSettings()
	}
	current, ok := s.settings.Load().(config.Settings)
	if !ok {
		return config.DefaultSettings()
	}
	return cloneRuntimeSettings(current)
}

// Snapshot returns a cloned runtime snapshot.
func (s *SettingsStore) Snapshot() Snapshot {
	if s == nil {
		snap, _ := compileRuntimeSettingsSnapshot(config.DefaultSettings())
		return snap
	}
	current, ok := s.snapshot.Load().(Snapshot)
	if !ok {
		snap, _ := compileRuntimeSettingsSnapshot(config.DefaultSettings())
		return snap
	}
	return cloneRuntimeSettingsSnapshot(current)
}

// Validate compiles the provided settings without mutating the store.
func (s *SettingsStore) Validate(next config.Settings) (Snapshot, error) {
	return compileRuntimeSettingsSnapshot(next)
}

// Publish replaces the active settings with the provided snapshot.
func (s *SettingsStore) Publish(next config.Settings, snapshot Snapshot) {
	if s == nil {
		return
	}
	s.settings.Store(cloneRuntimeSettings(next))
	s.snapshot.Store(cloneRuntimeSettingsSnapshot(snapshot))
}

func compileRuntimeSettingsSnapshot(settings config.Settings) (Snapshot, error) {
	normalized := cloneRuntimeSettings(settings)
	defaults := config.DefaultSettings()

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

func cloneRuntimeSettings(input config.Settings) config.Settings {
	clone := input
	clone.RequiredHeaders = cloneStringMap(input.RequiredHeaders)
	clone.ReasoningPoliciesMap = cloneStringMap(input.ReasoningPoliciesMap)
	clone.ReasoningPolicies = cloneReasoningPolicyRows(input.ReasoningPolicies)
	clone.ClaudeHaikuFallbackModels = cloneStringSliceRuntime(input.ClaudeHaikuFallbackModels)
	clone.ClaudeHaikuFallbackModelsUI = cloneHaikuFallbackUIRows(input.ClaudeHaikuFallbackModelsUI)
	return clone
}

func cloneRuntimeSettingsSnapshot(input Snapshot) Snapshot {
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

package runtimeconfig

import (
	"strings"

	"copilot-proxy/internal/config"
)

type Config struct {
	ListenAddr                        string
	UpstreamBase                      string
	RequiredHeaders                   map[string]string
	MaxRetries                        int
	RetryBackoff                      Duration
	RateLimitSeconds                  int
	MessagesAgentDetectionRequestMode bool
	ReasoningPoliciesMap              map[string]string
	ClaudeHaikuFallbackModels         []string
}

var defaultClaudeHaikuFallbackModels = []string{"gpt-5-mini", "grok-code-fast-1"}

func Default() Config {
	return Config{
		ListenAddr:                        config.DefaultListenAddr,
		UpstreamBase:                      config.CopilotAPIURL,
		RequiredHeaders:                   nil,
		MaxRetries:                        config.DefaultMaxRetries,
		RetryBackoff:                      NewDuration(config.DefaultRetryBackoff),
		RateLimitSeconds:                  0,
		MessagesAgentDetectionRequestMode: true,
		ReasoningPoliciesMap:              nil,
		ClaudeHaikuFallbackModels:         cloneStringSlice(defaultClaudeHaikuFallbackModels),
	}
}

func DefaultProxyHeaders() map[string]string {
	return map[string]string{
		"user-agent":             config.DefaultUserAgent,
		"copilot-integration-id": config.DefaultIntegrationID,
	}
}

func (cfg *Config) RequiredHeadersWithDefaults() map[string]string {
	defaults := DefaultProxyHeaders()
	if cfg == nil {
		return defaults
	}
	for key, value := range normalizeHeaders(cfg.RequiredHeaders) {
		defaults[key] = value
	}
	return defaults
}

func Clone(input Config) Config {
	return Config{
		ListenAddr:                        input.ListenAddr,
		UpstreamBase:                      input.UpstreamBase,
		RequiredHeaders:                   cloneStringMap(input.RequiredHeaders),
		MaxRetries:                        input.MaxRetries,
		RetryBackoff:                      input.RetryBackoff,
		RateLimitSeconds:                  input.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: input.MessagesAgentDetectionRequestMode,
		ReasoningPoliciesMap:              cloneStringMap(input.ReasoningPoliciesMap),
		ClaudeHaikuFallbackModels:         cloneStringSlice(input.ClaudeHaikuFallbackModels),
	}
}

func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		normalized[strings.ToLower(key)] = value
	}
	return normalized
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

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}

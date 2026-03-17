package transform

import (
	"strings"

	"copilot-proxy/internal/config"
)

// NormalizeLocalPath normalizes supported local API paths.
func NormalizeLocalPath(path string) string {
	switch strings.TrimSpace(path) {
	case config.ChatCompletionsPath:
		return config.ChatCompletionsPath
	case config.ResponsesPath:
		return config.ResponsesPath
	case config.MessagesPath:
		return config.MessagesPath
	default:
		return ""
	}
}

// NormalizeUpstreamPath normalizes supported upstream API paths.
func NormalizeUpstreamPath(path string) string {
	switch strings.TrimSpace(path) {
	case config.UpstreamChatCompletionsPath:
		return config.UpstreamChatCompletionsPath
	case config.UpstreamResponsesPath:
		return config.UpstreamResponsesPath
	case config.UpstreamMessagesPath:
		return config.UpstreamMessagesPath
	default:
		return ""
	}
}

// LocalToUpstream maps a local path to an upstream path.
func LocalToUpstream(localPath string) (string, bool) {
	switch NormalizeLocalPath(localPath) {
	case config.ChatCompletionsPath:
		return config.UpstreamChatCompletionsPath, true
	case config.ResponsesPath:
		return config.UpstreamResponsesPath, true
	case config.MessagesPath:
		return config.UpstreamMessagesPath, true
	default:
		return "", false
	}
}

// PickTargetEndpoint selects target upstream endpoint by policy:
// 1. For local /v1/chat/completions: always /chat/completions
// 2. For local /v1/responses: always /responses
// 3. For local /v1/messages:
//   - same endpoint as source request (if supported)
//   - /responses
//   - /v1/messages
//   - /chat/completions
//
// If model endpoints are missing or unrecognized for /v1/messages, it falls back to /chat/completions.
func PickTargetEndpoint(sourceLocalPath string, selectedModelEndpoints []string) string {
	switch NormalizeLocalPath(sourceLocalPath) {
	case config.ChatCompletionsPath:
		return config.UpstreamChatCompletionsPath
	case config.ResponsesPath:
		return config.UpstreamResponsesPath
	}

	normalized := normalizeAndUniqueUpstreamEndpoints(selectedModelEndpoints)
	if len(normalized) == 0 {
		return config.UpstreamChatCompletionsPath
	}

	if current, ok := LocalToUpstream(sourceLocalPath); ok && containsString(normalized, current) {
		return current
	}

	if containsString(normalized, config.UpstreamResponsesPath) {
		return config.UpstreamResponsesPath
	}
	if containsString(normalized, config.UpstreamMessagesPath) {
		return config.UpstreamMessagesPath
	}
	if containsString(normalized, config.UpstreamChatCompletionsPath) {
		return config.UpstreamChatCompletionsPath
	}
	return config.UpstreamChatCompletionsPath
}

func normalizeAndUniqueUpstreamEndpoints(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		normalized := NormalizeUpstreamPath(item)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

package routing

import (
	"strings"

	protocolpaths "copilot-proxy/internal/runtime/protocol/paths"
)

// NormalizeLocalPath normalizes supported local API paths.
func NormalizeLocalPath(path string) string {
	switch strings.TrimSpace(path) {
	case protocolpaths.ChatCompletionsPath:
		return protocolpaths.ChatCompletionsPath
	case protocolpaths.ResponsesPath:
		return protocolpaths.ResponsesPath
	case protocolpaths.MessagesPath:
		return protocolpaths.MessagesPath
	default:
		return ""
	}
}

// NormalizeUpstreamPath normalizes supported upstream API paths.
func NormalizeUpstreamPath(path string) string {
	switch strings.TrimSpace(path) {
	case protocolpaths.UpstreamChatCompletionsPath:
		return protocolpaths.UpstreamChatCompletionsPath
	case protocolpaths.UpstreamResponsesPath:
		return protocolpaths.UpstreamResponsesPath
	case protocolpaths.UpstreamMessagesPath:
		return protocolpaths.UpstreamMessagesPath
	default:
		return ""
	}
}

// LocalToUpstream maps a local path to an upstream path.
func LocalToUpstream(localPath string) (string, bool) {
	switch NormalizeLocalPath(localPath) {
	case protocolpaths.ChatCompletionsPath:
		return protocolpaths.UpstreamChatCompletionsPath, true
	case protocolpaths.ResponsesPath:
		return protocolpaths.UpstreamResponsesPath, true
	case protocolpaths.MessagesPath:
		return protocolpaths.UpstreamMessagesPath, true
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
// Unknown local paths return empty to allow static path mapping to decide.
func PickTargetEndpoint(sourceLocalPath string, selectedModelEndpoints []string) string {
	switch NormalizeLocalPath(sourceLocalPath) {
	case protocolpaths.ChatCompletionsPath:
		return protocolpaths.UpstreamChatCompletionsPath
	case protocolpaths.ResponsesPath:
		return protocolpaths.UpstreamResponsesPath
	case protocolpaths.MessagesPath:
		// Continue below to select compatible upstream endpoint.
	default:
		return ""
	}

	normalized := normalizeAndUniqueUpstreamEndpoints(selectedModelEndpoints)
	if len(normalized) == 0 {
		return protocolpaths.UpstreamChatCompletionsPath
	}

	if current, ok := LocalToUpstream(sourceLocalPath); ok && containsString(normalized, current) {
		return current
	}

	if containsString(normalized, protocolpaths.UpstreamResponsesPath) {
		return protocolpaths.UpstreamResponsesPath
	}
	if containsString(normalized, protocolpaths.UpstreamMessagesPath) {
		return protocolpaths.UpstreamMessagesPath
	}
	if containsString(normalized, protocolpaths.UpstreamChatCompletionsPath) {
		return protocolpaths.UpstreamChatCompletionsPath
	}
	return protocolpaths.UpstreamChatCompletionsPath
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

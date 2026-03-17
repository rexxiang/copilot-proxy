package middleware

import (
	"encoding/json"
	"strings"

	"copilot-proxy/internal/runtime/config"
)

// RequestParser extracts request information from request body.
type RequestParser interface {
	Parse(body []byte) RequestInfo
}

// ParseOptions controls request parsing behavior toggles.
type ParseOptions struct {
	MessagesAgentDetectionRequestMode bool
}

// ChatCompletionsParser parses OpenAI Chat Completions API format.
// Format: {"model": "...", "messages": [{"role": "...", "content": ...}]}
// Image type: "image_url" in content array
// Agent detection: checks if last message has role != "user" (e.g., "assistant", "system").
// Only the last message is checked because it represents the current conversation state.
type ChatCompletionsParser struct{}

func (p *ChatCompletionsParser) Parse(body []byte) RequestInfo {
	var req chatCompletionsRequest
	if json.Unmarshal(body, &req) != nil || len(req.Messages) == 0 {
		return emptyRequestInfo()
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	return RequestInfo{
		IsVision: checkChatCompletionsVision(req.Messages),
		IsAgent:  isNonUserRole(lastMsg.Role),
		Model:    req.Model,
	}
}

// ResponsesParser parses OpenAI Responses API format.
// Format: {"model": "...", "input": [{"role": "...", "content": ...}]}
// Image type: "input_image" in content array
// Agent detection: checks if last input item has role != "user".
// Only the last item is checked because it represents the current conversation state.
type ResponsesParser struct{}

func (p *ResponsesParser) Parse(body []byte) RequestInfo {
	var req responsesRequest
	if json.Unmarshal(body, &req) != nil || len(req.Input) == 0 {
		return emptyRequestInfo()
	}

	lastItem := req.Input[len(req.Input)-1]
	return RequestInfo{
		IsVision: checkResponsesVision(req.Input),
		IsAgent:  isNonUserRole(lastItem.Role),
		Model:    req.Model,
	}
}

// AnthropicMessagesParser parses Anthropic Messages API format.
// Format: {"model": "...", "messages": [{"role": "...", "content": ...}], "system": "..."}
// Image type: "image" in content array
// Agent detection supports request/session modes.
type AnthropicMessagesParser struct {
	RequestModeAgentDetection bool
}

func (p *AnthropicMessagesParser) Parse(body []byte) RequestInfo {
	var req anthropicRequest
	if json.Unmarshal(body, &req) != nil || len(req.Messages) == 0 {
		return emptyRequestInfo()
	}

	return RequestInfo{
		IsVision: checkAnthropicVision(req.Messages),
		IsAgent:  isAnthropicAgent(req.Messages, p.RequestModeAgentDetection),
		Model:    req.Model,
	}
}

// ParseRequestByPath selects the appropriate parser based on request path.
func ParseRequestByPath(path string, body []byte) RequestInfo {
	return ParseRequestByPathWithOptions(path, body, ParseOptions{
		MessagesAgentDetectionRequestMode: true,
	})
}

// ParseRequestByPathWithOptions selects the appropriate parser based on request path and options.
func ParseRequestByPathWithOptions(path string, body []byte, options ParseOptions) RequestInfo {
	if len(body) == 0 {
		return emptyRequestInfo()
	}
	parser, ok := parserByPath(path, options)
	if !ok {
		return emptyRequestInfo()
	}
	return parser.Parse(body)
}

func emptyRequestInfo() RequestInfo {
	return RequestInfo{}
}

func parserByPath(path string, options ParseOptions) (RequestParser, bool) {
	switch path {
	case config.ChatCompletionsPath:
		return &ChatCompletionsParser{}, true
	case config.ResponsesPath:
		return &ResponsesParser{}, true
	case config.MessagesPath:
		return &AnthropicMessagesParser{RequestModeAgentDetection: options.MessagesAgentDetectionRequestMode}, true
	default:
		return nil, false
	}
}

// chatMessage represents a single message in OpenAI Chat Completions format.
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// chatCompletionsRequest represents the OpenAI Chat Completions API request structure.
type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// responseInputItem represents a single input item in OpenAI Responses format.
type responseInputItem struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// responsesRequest represents the OpenAI Responses API request structure.
type responsesRequest struct {
	Model string              `json:"model"`
	Input []responseInputItem `json:"input"`
}

// anthropicMessage represents a single message in Anthropic format.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// anthropicRequest represents the Anthropic Messages API request structure.
type anthropicRequest struct {
	Model    string             `json:"model"`
	Messages []anthropicMessage `json:"messages"`
}

// forEachContentPart iterates over content array parts, calling fn for each map element.
// Returns true if fn returns true for any part.
func forEachContentPart(content any, fn func(part map[string]any) bool) bool {
	parts, ok := content.([]any)
	if !ok {
		return false
	}
	for _, part := range parts {
		if m, ok := part.(map[string]any); ok {
			if fn(m) {
				return true
			}
		}
	}
	return false
}

// hasContentOfType checks if content array contains a part with the specified type.
func hasContentOfType(content any, targetType string) bool {
	return forEachContentPart(content, func(part map[string]any) bool {
		return part["type"] == targetType
	})
}

// isNonUserRole returns true if role is not "user".
func isNonUserRole(role string) bool {
	return role != roleUser
}

// checkChatCompletionsVision checks if any message contains image_url content (OpenAI Chat format).
func checkChatCompletionsVision(messages []chatMessage) bool {
	for _, msg := range messages {
		if hasContentOfType(msg.Content, contentTypeImageURL) {
			return true
		}
	}
	return false
}

// checkResponsesVision checks if any input item contains input_image content (OpenAI Responses format).
func checkResponsesVision(items []responseInputItem) bool {
	for _, item := range items {
		if hasContentOfType(item.Content, contentTypeInputImage) {
			return true
		}
	}
	return false
}

// checkAnthropicVision checks if any message contains image content (Anthropic format).
// This includes images nested inside tool_result content.
func checkAnthropicVision(messages []anthropicMessage) bool {
	for _, msg := range messages {
		if hasAnthropicImage(msg.Content) {
			return true
		}
	}
	return false
}

// isAnthropicAgent determines if the request is from an agent.
// Rules:
// 1. All messages are "user" and count >= 2 -> agent (init sequence)
// 2. Request mode (mode=true):
//   - Last message role != "user" -> agent
//   - Last message content has type suffix tool_use/tool_result -> agent
//
// 3. Session mode (mode=false):
//   - Any message role != "user" -> agent
//   - Any message content has type suffix tool_result -> agent
func isAnthropicAgent(messages []anthropicMessage, requestMode bool) bool {
	if len(messages) >= minMessagesForInitSequence && allAnthropicRolesUser(messages) {
		return true
	}

	if requestMode {
		last := messages[len(messages)-1]
		if last.Role != roleUser {
			return true
		}
		return hasToolUseOrToolResultContent(last.Content)
	}

	for _, msg := range messages {
		if msg.Role != roleUser {
			return true
		}
		if hasToolResultContent(msg.Content) {
			return true
		}
	}
	return false
}

// hasToolResultContent checks if content contains any type ending with "tool_result".
// This matches: tool_result, mcp_tool_result, server_tool_result, etc.
func hasToolResultContent(content any) bool {
	return forEachContentPart(content, func(part map[string]any) bool {
		if t, ok := part["type"].(string); ok {
			return strings.HasSuffix(t, "tool_result")
		}
		return false
	})
}

func hasToolUseOrToolResultContent(content any) bool {
	return forEachContentPart(content, func(part map[string]any) bool {
		typ, ok := part["type"].(string)
		if !ok {
			return false
		}
		return strings.HasSuffix(typ, contentTypeToolUse) || strings.HasSuffix(typ, contentTypeToolResult)
	})
}

func allAnthropicRolesUser(messages []anthropicMessage) bool {
	for _, msg := range messages {
		if msg.Role != roleUser {
			return false
		}
	}
	return true
}

// hasAnthropicImage checks for image type in Anthropic format, including nested in tool_result.
func hasAnthropicImage(content any) bool {
	parts, ok := content.([]any)
	if !ok {
		return false
	}
	for _, part := range parts {
		m, ok := part.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := m["type"].(string)
		// Direct image content
		if partType == contentTypeImage {
			return true
		}
		// Nested image inside tool_result content
		if partType == contentTypeToolResult {
			if nested, ok := m["content"].([]any); ok {
				for _, nestedPart := range nested {
					if nm, ok := nestedPart.(map[string]any); ok {
						if nm["type"] == contentTypeImage {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

const (
	// OpenAI Chat Completions format.
	contentTypeImageURL = "image_url"
	// OpenAI Responses format.
	contentTypeInputImage = "input_image"
	// Anthropic Messages format.
	contentTypeImage      = "image"
	contentTypeToolUse    = "tool_use"
	contentTypeToolResult = "tool_result"
	// Role identifiers.
	roleUser = "user"
	// Parsing thresholds.
	minMessagesForInitSequence = 2
)

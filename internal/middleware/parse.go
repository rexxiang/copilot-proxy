package middleware

import requestctx "copilot-proxy/internal/runtime/request"

// RequestParser aliases runtime request parser during migration.
type RequestParser = requestctx.Parser

// ParseOptions aliases runtime parse options during migration.
type ParseOptions = requestctx.ParseOptions

// ChatCompletionsParser aliases runtime chat parser during migration.
type ChatCompletionsParser = requestctx.ChatCompletionsParser

// ResponsesParser aliases runtime responses parser during migration.
type ResponsesParser = requestctx.ResponsesParser

// AnthropicMessagesParser aliases runtime messages parser during migration.
type AnthropicMessagesParser = requestctx.AnthropicMessagesParser

// ParseRequestByPath selects the appropriate parser based on request path.
func ParseRequestByPath(path string, body []byte) RequestInfo {
	return requestctx.ParseRequestByPath(path, body)
}

// ParseRequestByPathWithOptions selects the appropriate parser based on request path and options.
func ParseRequestByPathWithOptions(path string, body []byte, options ParseOptions) RequestInfo {
	return requestctx.ParseRequestByPathWithOptions(path, body, options)
}

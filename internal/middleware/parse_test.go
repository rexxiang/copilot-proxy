package middleware

import "testing"

func TestChatCompletionsParser(t *testing.T) {
	parser := &ChatCompletionsParser{}

	tests := []struct {
		name     string
		body     string
		model    string
		isAgent  bool
		isVision bool
	}{
		{
			name:     "user message",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`,
			model:    "gpt-4",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "assistant last",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			model:    "gpt-4",
			isAgent:  true,
			isVision: false,
		},
		{
			name: "assistant in middle",
			body: `{"model":"gpt-4","messages":[` +
				`{"role":"user","content":"hi"},` +
				`{"role":"assistant","content":"hello"},` +
				`{"role":"user","content":"bye"}]}`,
			model:    "gpt-4",
			isAgent:  false, // last message is user, not agent
			isVision: false,
		},
		{
			name:     "system message only",
			body:     `{"model":"gpt-4","messages":[{"role":"system","content":"you are helpful"}]}`,
			model:    "gpt-4",
			isAgent:  true, // system is not user
			isVision: false,
		},
		{
			name:     "with image_url",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			model:    "gpt-4",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "empty messages",
			body:     `{"model":"gpt-4","messages":[]}`,
			model:    "",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "invalid json",
			body:     `not json`,
			model:    "",
			isAgent:  false,
			isVision: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parser.Parse([]byte(tc.body))
			if info.Model != tc.model {
				t.Errorf("Model: got %q, want %q", info.Model, tc.model)
			}
			if info.IsAgent != tc.isAgent {
				t.Errorf("IsAgent: got %v, want %v", info.IsAgent, tc.isAgent)
			}
			if info.IsVision != tc.isVision {
				t.Errorf("IsVision: got %v, want %v", info.IsVision, tc.isVision)
			}
		})
	}
}

func TestResponsesParser(t *testing.T) {
	parser := &ResponsesParser{}

	tests := []struct {
		name     string
		body     string
		model    string
		isAgent  bool
		isVision bool
	}{
		{
			name:     "user message",
			body:     `{"model":"gpt-4o","input":[{"role":"user","content":"hello"}]}`,
			model:    "gpt-4o",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "assistant last",
			body:     `{"model":"gpt-4o","input":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			model:    "gpt-4o",
			isAgent:  true,
			isVision: false,
		},
		{
			name:     "with input_image",
			body:     `{"model":"gpt-4o","input":[{"role":"user","content":[{"type":"input_image"}]}]}`,
			model:    "gpt-4o",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "empty input",
			body:     `{"model":"gpt-4o","input":[]}`,
			model:    "",
			isAgent:  false,
			isVision: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parser.Parse([]byte(tc.body))
			if info.Model != tc.model {
				t.Errorf("Model: got %q, want %q", info.Model, tc.model)
			}
			if info.IsAgent != tc.isAgent {
				t.Errorf("IsAgent: got %v, want %v", info.IsAgent, tc.isAgent)
			}
			if info.IsVision != tc.isVision {
				t.Errorf("IsVision: got %v, want %v", info.IsVision, tc.isVision)
			}
		})
	}
}

func TestAnthropicMessagesParser(t *testing.T) {
	parser := &AnthropicMessagesParser{}

	tests := []struct {
		name     string
		body     string
		model    string
		isAgent  bool
		isVision bool
	}{
		{
			name:     "user message",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":"hello"}]}`,
			model:    "claude-3-opus",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "with system",
			body:     `{"model":"claude-3-opus","system":"You are helpful","messages":[{"role":"user","content":"hello"}]}`,
			model:    "claude-3-opus",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "assistant last",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]}`,
			model:    "claude-3-opus",
			isAgent:  true,
			isVision: false,
		},
		{
			name:     "with image type",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64"}}]}]}`,
			model:    "claude-3-opus",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "image_url not detected",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  false,
			isVision: false, // Anthropic parser only looks for "image" type
		},
		{
			name: "tool_result in user message",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":[` +
				`{"type":"tool_result","tool_use_id":"123","content":"result"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // tool_result content indicates agent context
			isVision: false,
		},
		{
			name: "user message with text and tool_result",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":[` +
				`{"type":"text","text":"hello"},` +
				`{"type":"tool_result","tool_use_id":"123"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // has tool_result content = agent (regardless of other content types)
			isVision: false,
		},
		{
			name: "nested image in tool_result",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":[` +
				`{"type":"tool_result","tool_use_id":"123","content":[` +
				`{"type":"image","source":{"type":"base64"}}]}]}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // tool_result suffix = agent
			isVision: true, // nested image detected
		},
		{
			name: "mcp_tool_result in user message",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":[` +
				`{"type":"mcp_tool_result","tool_use_id":"456","content":"result"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // ends with tool_result = agent
			isVision: false,
		},
		{
			name: "server_tool_result in user message",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":[` +
				`{"type":"server_tool_result","tool_use_id":"789","content":"result"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // ends with tool_result = agent
			isVision: false,
		},
		{
			name:     "tool_use in user message - not agent",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":[{"type":"tool_use","id":"123","name":"test"}]}]}`,
			model:    "claude-3-opus",
			isAgent:  false, // tool_use does not end with tool_result
			isVision: false,
		},
		{
			name: "assistant message in middle",
			body: `{"model":"claude-3-opus","messages":[` +
				`{"role":"user","content":"hello"},` +
				`{"role":"assistant","content":"hi"},` +
				`{"role":"user","content":"bye"}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // any non-user role = agent
			isVision: false,
		},
		{
			name:     "multiple user messages - init sequence",
			body:     `{"model":"claude-3-opus","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"actual question"}]}`,
			model:    "claude-3-opus",
			isAgent:  true, // multiple all-user messages = init sequence
			isVision: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parser.Parse([]byte(tc.body))
			if info.Model != tc.model {
				t.Errorf("Model: got %q, want %q", info.Model, tc.model)
			}
			if info.IsAgent != tc.isAgent {
				t.Errorf("IsAgent: got %v, want %v", info.IsAgent, tc.isAgent)
			}
			if info.IsVision != tc.isVision {
				t.Errorf("IsVision: got %v, want %v", info.IsVision, tc.isVision)
			}
		})
	}
}

func TestAnthropicMessagesParser_DisableInitSequence(t *testing.T) {
	parser := &AnthropicMessagesParser{DisableInitSequenceDetection: true}
	body := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"msg1"},{"role":"user","content":"msg2"}]}`)

	info := parser.Parse(body)

	if info.IsAgent {
		t.Error("expected IsAgent=false when DisableInitSequenceDetection=true")
	}
}

func TestIsAnthropicAgentContent_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		isAgent bool
	}{
		{
			name:    "nil content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":null}]}`,
			isAgent: false,
		},
		{
			name:    "empty array content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":[]}]}`,
			isAgent: false,
		},
		{
			name:    "number content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":123}]}`,
			isAgent: false,
		},
		{
			name:    "object content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":{"key":"value"}}]}`,
			isAgent: false,
		},
		{
			name:    "boolean content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":true}]}`,
			isAgent: false,
		},
		{
			name:    "string array content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":["hello","world"]}]}`,
			isAgent: false,
		},
		{
			name:    "number array content",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":[1,2,3]}]}`,
			isAgent: false,
		},
		{
			name:    "mixed array with string",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":[{"type":"text"},"string"]}]}`,
			isAgent: false,
		},
		{
			name:    "part without type field",
			body:    `{"model":"claude-3","messages":[{"role":"user","content":[{"key":"value"}]}]}`,
			isAgent: false,
		},
	}

	parser := &AnthropicMessagesParser{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := parser.Parse([]byte(tc.body))
			if info.IsAgent != tc.isAgent {
				t.Errorf("IsAgent: got %v, want %v", info.IsAgent, tc.isAgent)
			}
		})
	}
}

func TestParseRequestByPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		body     string
		model    string
		isAgent  bool
		isVision bool
	}{
		{
			name:     "chat completions path with image_url",
			path:     "/v1/chat/completions",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			model:    "gpt-4",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "chat completions path with image type ignored",
			path:     "/v1/chat/completions",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":[{"type":"image"}]}]}`,
			model:    "gpt-4",
			isAgent:  false,
			isVision: false, // ChatCompletionsParser only looks for image_url
		},
		{
			name:     "responses path with input_image",
			path:     "/v1/responses",
			body:     `{"model":"gpt-4o","input":[{"role":"user","content":[{"type":"input_image"}]}]}`,
			model:    "gpt-4o",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "messages path with image",
			path:     "/v1/messages",
			body:     `{"model":"claude-3","messages":[{"role":"user","content":[{"type":"image"}]}]}`,
			model:    "claude-3",
			isAgent:  false,
			isVision: true,
		},
		{
			name:     "messages path with image_url ignored",
			path:     "/v1/messages",
			body:     `{"model":"claude-3","messages":[{"role":"user","content":[{"type":"image_url"}]}]}`,
			model:    "claude-3",
			isAgent:  false,
			isVision: false, // AnthropicMessagesParser only looks for image
		},
		{
			name:     "messages init sequence defaults to user",
			path:     "/v1/messages",
			body:     `{"model":"claude-3","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"question"}]}`,
			model:    "claude-3",
			isAgent:  false,
			isVision: false,
		},
		{
			name:     "unknown path returns empty",
			path:     "/unknown",
			body:     `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`,
			model:    "",
			isAgent:  false,
			isVision: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := ParseRequestByPath(tc.path, []byte(tc.body))
			if info.Model != tc.model {
				t.Errorf("Model: got %q, want %q", info.Model, tc.model)
			}
			if info.IsAgent != tc.isAgent {
				t.Errorf("IsAgent: got %v, want %v", info.IsAgent, tc.isAgent)
			}
			if info.IsVision != tc.isVision {
				t.Errorf("IsVision: got %v, want %v", info.IsVision, tc.isVision)
			}
		})
	}
}

func TestParseRequestByPathWithOptions_EnableMessagesInitSeqAgent(t *testing.T) {
	body := []byte(`{"model":"claude-3","messages":[{"role":"user","content":"system prompt"},{"role":"user","content":"question"}]}`)

	info := ParseRequestByPathWithOptions("/v1/messages", body, ParseOptions{
		MessagesInitSeqAgent: true,
	})

	if !info.IsAgent {
		t.Fatalf("expected IsAgent=true when messages_init_seq_agent enabled")
	}
}

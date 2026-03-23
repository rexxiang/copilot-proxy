package messages

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

const (
	testSearchName = "search"
	sseDoneToken   = "[DONE]"
	sseDoneLine    = "data: [DONE]"
)

func TestMessagesToChatRequestConvertsTools(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"tools":[` +
		`{"name":"search","description":"do search","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	tools, ok := parsed["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected tools array, got %v", parsed["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("expected tool object, got %T", tools[0])
	}
	if tool["type"] != "function" {
		t.Fatalf("expected tool type function, got %v", tool["type"])
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok {
		t.Fatalf("expected function object, got %T", tool["function"])
	}
	if fn["name"] != testSearchName {
		t.Fatalf("expected function name %s, got %v", testSearchName, fn["name"])
	}
	if _, ok := fn["parameters"]; !ok {
		t.Fatalf("expected function parameters")
	}
}

func TestMessagesToChatRequestConvertsSystemArray(t *testing.T) {
	reqBody := `{"model":"gpt-4o","system":[` +
		`{"type":"text","text":"a"},{"type":"text","text":"b"}],` +
		`"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages array")
	}
	first, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected message object")
	}
	if first["role"] != "system" {
		t.Fatalf("expected system role, got %v", first["role"])
	}
	contentParts, ok := first["content"].([]any)
	if !ok || len(contentParts) != 2 {
		t.Fatalf("expected system text parts array, got %#v", first["content"])
	}
	firstPart, ok := contentParts[0].(map[string]any)
	if !ok || firstPart["type"] != anthropicTypeText || firstPart["text"] != "a" {
		t.Fatalf("unexpected first system content part: %#v", contentParts[0])
	}
	secondPart, ok := contentParts[1].(map[string]any)
	if !ok || secondPart["type"] != anthropicTypeText || secondPart["text"] != "b" {
		t.Fatalf("unexpected second system content part: %#v", contentParts[1])
	}
}

func TestMessagesToChatRequestKeepsToolResultBeforeUserText(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"tool_result","tool_use_id":"tool1","content":"result"},` +
		`{"type":"text","text":"question"}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) < 2 {
		t.Fatalf("expected tool + user messages")
	}
	first, _ := msgs[0].(map[string]any)
	second, _ := msgs[1].(map[string]any)
	if first["role"] != anthropicToolChoice {
		t.Fatalf("expected tool result first, got %v", first["role"])
	}
	if second["role"] != "user" {
		t.Fatalf("expected user message second, got %v", second["role"])
	}
}

func TestMessagesToChatRequestConvertsToolChoice(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],` +
		`"tool_choice":{"type":"tool","name":"search"}}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	choice, ok := parsed["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_choice object, got %T", parsed["tool_choice"])
	}
	if choice["type"] != "function" {
		t.Fatalf("expected function tool_choice, got %v", choice["type"])
	}
	fn, ok := choice["function"].(map[string]any)
	if !ok || fn["name"] != testSearchName {
		t.Fatalf("expected function name %s, got %v", testSearchName, fn)
	}
}

func TestMessagesToChatRequestSkipsInvalidToolChoiceShape(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],` +
		`"tool_choice":"auto"}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if _, ok := parsed["tool_choice"]; ok {
		t.Fatalf("expected tool_choice omitted for invalid shape, got %v", parsed["tool_choice"])
	}
}

func TestMessagesToChatRequestAssistantDropsThinkingFromToolCallContent(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"assistant","content":[` +
		`{"type":"text","text":"visible"},` +
		`{"type":"thinking","thinking":"private thoughts"},` +
		`{"type":"tool_use","id":"call_1","name":"search","input":{"q":"hi"}}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(parsed.Messages))
	}
	msg := parsed.Messages[0]
	if msg.Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", msg.Role)
	}
	content, ok := msg.Content.([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected assistant content as one text part, got %v", msg.Content)
	}
	contentPart, ok := content[0].(map[string]any)
	if !ok || contentPart["type"] != anthropicTypeText || contentPart["text"] != "visible" {
		t.Fatalf("expected only visible text part, got %#v", content[0])
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != testSearchName {
		t.Fatalf("expected tool call name search, got %q", msg.ToolCalls[0].Function.Name)
	}
}

func TestMessagesToChatRequestDropsThinkingWhenUserHasImage(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"caption"},` +
		`{"type":"thinking","thinking":"private context"},` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if len(parsed.Messages) != 1 {
		t.Fatalf("expected one user message, got %d", len(parsed.Messages))
	}
	parts := parsed.Messages[0].Content
	if len(parts) != 2 {
		t.Fatalf("expected 2 content parts, got %#v", parts)
	}
	if parts[0].Type != anthropicTypeText || parts[0].Text != "caption" {
		t.Fatalf("expected first text part caption, got %#v", parts[0])
	}
	if parts[1].Type != "image_url" || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("expected image_url data URL, got %#v", parts[1])
	}
}

func TestMessagesToChatRequestUsesMetadataUser(t *testing.T) {
	reqBody := `{"model":"gpt-4o","metadata":{"user_id":"u1"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if parsed["user"] != "u1" {
		t.Fatalf("expected user u1, got %v", parsed["user"])
	}
}

func TestMessagesToChatRequestBuildsReasoningEffortFromOutputConfig(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if parsed["reasoning_effort"] != normalizedEffortHigh {
		t.Fatalf("expected reasoning_effort=high, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestMapsMaxEffortToHigh(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"max"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if parsed["reasoning_effort"] != normalizedEffortHigh {
		t.Fatalf("expected reasoning_effort=high for max, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestMapsXHighToHigh(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"xhigh"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if parsed["reasoning_effort"] != normalizedEffortHigh {
		t.Fatalf("expected reasoning_effort=high for xhigh, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestMapsMinimalEffortToLow(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"minimal"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if parsed["reasoning_effort"] != normalizedEffortLow {
		t.Fatalf("expected reasoning_effort=low for minimal, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestMapsUnknownEffortToNone(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"unknown"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("expected no reasoning_effort for unknown effort, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestWithOptionsRequiresSupportedEffort(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequestWithOptions([]byte(reqBody), MessagesReasoningOptions{
		SupportedReasoningEffort: nil,
	})
	if !ok {
		t.Fatalf("MessagesToChatRequestWithOptions failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if _, exists := parsed["reasoning_effort"]; exists {
		t.Fatalf("expected no reasoning_effort when supported list is empty, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestSkipsReasoningEffortWhenMissingOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing output config",
			body: `{"model":"gpt-4o","thinking":{"type":"enabled"},"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "top level ignored",
			body: `{"model":"gpt-4o","reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "empty effort",
			body: `{"model":"gpt-4o","output_config":{"effort":""},"messages":[{"role":"user","content":"hi"}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			converted, ok := MessagesToChatRequest([]byte(tc.body))
			if !ok {
				t.Fatalf("MessagesToChatRequest failed")
			}

			var parsed map[string]any
			if err := json.Unmarshal(converted, &parsed); err != nil {
				t.Fatalf("unmarshal converted: %v", err)
			}
			if _, ok := parsed["reasoning_effort"]; ok {
				t.Fatalf("expected no reasoning_effort, got %v", parsed["reasoning_effort"])
			}
		})
	}
}

func TestMessagesToChatRequestSkipsReasoningEffortWhenInvalid(t *testing.T) {
	reqBody := `{"model":"gpt-4o","output_config":{"effort":"ultra"},"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("expected no reasoning_effort for invalid value, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesToChatRequestConvertsStopSequences(t *testing.T) {
	reqBody := `{"model":"gpt-4o","stop_sequences":["END"],"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	stop, ok := parsed["stop"].([]any)
	if !ok || len(stop) != 1 || stop[0] != "END" {
		t.Fatalf("expected stop sequences, got %v", parsed["stop"])
	}
}

func TestMessagesToChatRequestPreservesEmptyStopSequences(t *testing.T) {
	reqBody := `{"model":"gpt-4o","stop_sequences":[],"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	stop, exists := parsed["stop"]
	if !exists {
		t.Fatalf("expected stop key preserved for empty stop_sequences, got %v", parsed)
	}
	stopList, ok := stop.([]any)
	if !ok || len(stopList) != 0 {
		t.Fatalf("expected empty stop array, got %T %v", stop, stop)
	}
}

func TestMessagesToChatRequestPreservesExplicitStreamFalse(t *testing.T) {
	reqBody := `{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	stream, exists := parsed["stream"]
	if !exists {
		t.Fatalf("expected stream key preserved for explicit false, got %v", parsed)
	}
	streamBool, ok := stream.(bool)
	if !ok || streamBool {
		t.Fatalf("expected stream=false, got %T %v", stream, stream)
	}
}

func TestMessagesToChatRequestToolResultOnlySkipsUserMessage(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool1","content":"result"}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected only tool message, got %v", parsed["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != anthropicToolChoice {
		t.Fatalf("expected tool message, got %v", first["role"])
	}
}

func TestMessagesToChatRequestToolResultArrayBecomesJoinedText(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":` +
		`[{"type":"tool_result","tool_use_id":"tool1","content":` +
		`[{"type":"text","text":"a"},{"type":"text","text":"b"}]}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages array, got %v", parsed["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != anthropicToolChoice {
		t.Fatalf("expected tool message, got %v", first["role"])
	}
	contentParts, ok := first["content"].([]any)
	if !ok || len(contentParts) != 2 {
		t.Fatalf("expected tool content text parts, got %#v", first["content"])
	}
	firstPart, _ := contentParts[0].(map[string]any)
	secondPart, _ := contentParts[1].(map[string]any)
	if firstPart["type"] != anthropicTypeText || firstPart["text"] != "a" {
		t.Fatalf("unexpected first tool content part: %#v", firstPart)
	}
	if secondPart["type"] != anthropicTypeText || secondPart["text"] != "b" {
		t.Fatalf("unexpected second tool content part: %#v", secondPart)
	}
}

func TestMessagesToChatRequestPreservesUserBlockOrderWithToolResultInMiddle(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"before"},` +
		`{"type":"tool_result","tool_use_id":"tool1","content":"result"},` +
		`{"type":"text","text":"after"}]}]}`
	converted, ok := MessagesToChatRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToChatRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	msgs, ok := parsed["messages"].([]any)
	if !ok || len(msgs) != 3 {
		t.Fatalf("expected user -> tool -> user messages, got %#v", parsed["messages"])
	}

	first, _ := msgs[0].(map[string]any)
	second, _ := msgs[1].(map[string]any)
	third, _ := msgs[2].(map[string]any)

	if first["role"] != anthropicRoleUser || second["role"] != anthropicToolChoice || third["role"] != anthropicRoleUser {
		t.Fatalf("unexpected role order: %v, %v, %v", first["role"], second["role"], third["role"])
	}
}

func TestChatToMessagesResponseConvertsCachedTokenUsage(t *testing.T) {
	resp := `{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` +
		`{"prompt_tokens":10,"completion_tokens":2,` +
		`"prompt_tokens_details":{"cached_tokens":3}}}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	usage, ok := parsed["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage")
	}
	if usage["input_tokens"] != float64(7) {
		t.Fatalf("expected input_tokens 7, got %v", usage["input_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(3) {
		t.Fatalf("expected cache_read_input_tokens 3, got %v", usage["cache_read_input_tokens"])
	}
}

func TestChatToMessagesResponsePreservesUsageFieldsWhenCachedTokensZero(t *testing.T) {
	resp := `{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` +
		`{"prompt_tokens":10,"completion_tokens":2,` +
		`"prompt_tokens_details":{"cached_tokens":0}}}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	usage, ok := parsed["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage")
	}
	if _, ok := usage["cache_read_input_tokens"]; !ok {
		t.Fatalf("expected cache_read_input_tokens to be present")
	}
}

func TestChatToMessagesResponsePreservesID(t *testing.T) {
	resp := `{"id":"chatcmpl-123","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` +
		`{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if parsed["id"] != "chatcmpl-123" {
		t.Fatalf("expected id preserved, got %v", parsed["id"])
	}
}

func TestChatToMessagesResponseMapsContentFilterToEndTurn(t *testing.T) {
	resp := `{"id":"chatcmpl-8","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","content":"blocked"},"finish_reason":"content_filter"}]}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if parsed["stop_reason"] != "end_turn" {
		t.Fatalf("expected stop_reason end_turn for content_filter, got %v", parsed["stop_reason"])
	}
}

func TestChatToMessagesResponseAllowsEmptyChoices(t *testing.T) {
	resp := `{"id":"chatcmpl-empty","model":"gpt-4o","choices":[]}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed for empty choices")
	}
	var parsed struct {
		Content []map[string]any `json:"content"`
		Usage   map[string]any   `json:"usage"`
	}
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if len(parsed.Content) != 0 {
		t.Fatalf("expected empty content array, got %v", parsed.Content)
	}
	if parsed.Usage["input_tokens"] != float64(0) || parsed.Usage["output_tokens"] != float64(0) {
		t.Fatalf("expected zero usage defaults, got %v", parsed.Usage)
	}
	if _, ok := parsed.Usage["cache_read_input_tokens"]; ok {
		t.Fatalf("expected no cache_read_input_tokens when missing upstream field, got %v", parsed.Usage)
	}
}

func TestChatToMessagesResponseFailsWhenToolArgumentsInvalidJSON(t *testing.T) {
	resp := `{"id":"chatcmpl-9","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","tool_calls":[{"id":"call_1","type":"function",` +
		`"function":{"name":"search","arguments":"{not-json}"}}]},` +
		`"finish_reason":"tool_calls"}]}`
	if _, ok := ChatToMessagesResponse([]byte(resp)); ok {
		t.Fatalf("expected conversion failure for invalid tool arguments JSON")
	}
}

func TestChatToMessagesResponseDefaultsUsageWhenMissing(t *testing.T) {
	resp := `{"id":"chatcmpl-10","model":"gpt-4o","choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	usage, ok := parsed["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage object in response, got %T", parsed["usage"])
	}
	if usage["input_tokens"] != float64(0) || usage["output_tokens"] != float64(0) {
		t.Fatalf("expected zero usage defaults, got %v", usage)
	}
	if _, ok := usage["cache_read_input_tokens"]; ok {
		t.Fatalf("expected cache_read_input_tokens omitted when upstream cache field missing, got %v", usage)
	}
}

func TestChatToMessagesResponsePreservesReasoningAsThinkingBlocks(t *testing.T) {
	resp := `{"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"message":` +
		`{"role":"assistant","reasoning_text":"top-thought","content":[` +
		`{"type":"text","text":"visible"},` +
		`{"type":"reasoning_text","text":"hidden"},` +
		`{"type":"thinking","thinking":"hidden2"},` +
		`{"type":"refusal","text":"policy"}]},"finish_reason":"stop"}]}`
	translated, ok := ChatToMessagesResponse([]byte(resp))
	if !ok {
		t.Fatalf("ChatToMessagesResponse failed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	content, ok := parsed["content"].([]any)
	if !ok {
		t.Fatalf("expected content array, got %T", parsed["content"])
	}
	var foundVisible bool
	var foundTopThinking bool
	var foundInnerThinking bool
	var foundRefusalText bool
	for _, item := range content {
		block, _ := item.(map[string]any)
		blockType, _ := block["type"].(string)
		switch blockType {
		case anthropicTypeText:
			text, _ := block["text"].(string)
			if text == "visible" {
				foundVisible = true
			}
			if text == "policy" {
				foundRefusalText = true
			}
		case anthropicTypeThinking:
			thinking, _ := block["thinking"].(string)
			if strings.Contains(thinking, "top-thought") {
				foundTopThinking = true
			}
			if strings.Contains(thinking, "hidden") {
				foundInnerThinking = true
			}
		}
	}
	if !foundVisible || !foundTopThinking || !foundInnerThinking || !foundRefusalText {
		t.Fatalf("expected visible/refusal/thinking blocks preserved, got %s", string(translated))
	}
}

func TestTranslateChatSSEToMessagesPreservesCachedTokensWhenZero(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-6","model":"gpt-4o","choices":[{"index":0,` +
			`"delta":{"role":"assistant"}}],"usage":{"prompt_tokens":1,` +
			`"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":0}}}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"stop"}],"usage":` +
			`{"prompt_tokens":1,"completion_tokens":1,` +
			`"prompt_tokens_details":{"cached_tokens":0}}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	if !strings.Contains(string(respBytes), `"cache_read_input_tokens":0`) {
		t.Fatalf("expected cache_read_input_tokens in stream events, got %s", string(respBytes))
	}
}

func TestTranslateChatSSEToMessagesOmitsCacheWhenMissing(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-11","model":"gpt-4o","choices":[{"index":0,` +
			`"delta":{"role":"assistant"}}],"usage":{"prompt_tokens":2,` +
			`"completion_tokens":0}}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	text := string(respBytes)
	if strings.Contains(text, `"cache_read_input_tokens"`) {
		t.Fatalf("expected cache_read_input_tokens omitted when missing upstream field, got %s", text)
	}
}

func TestTranslateChatSSEToMessagesMapsContentFilterToEndTurn(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-12","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"content_filter"}],"usage":{"prompt_tokens":1,"completion_tokens":0}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	if !strings.Contains(string(respBytes), `"stop_reason":"end_turn"`) {
		t.Fatalf("expected stop_reason end_turn for content_filter, got %s", string(respBytes))
	}
}

func TestTranslateChatSSEToMessagesToolThenTextBlockOrder(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-13","model":"gpt-4o","choices":[{"index":0,` +
			`"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function",` +
			`"function":{"name":"search","arguments":""}}]}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"hi\"}"}}]}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"content":"done"}}]}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	events := decodeSSEDataEvents(t, string(respBytes))

	mustContainInOrder(t, events,
		"type=content_block_start,index=0,block=tool_use",
		"type=content_block_delta,index=0,delta=input_json_delta",
		"type=content_block_stop,index=0",
		"type=content_block_start,index=1,block=text",
		"type=content_block_delta,index=1,delta=text_delta",
	)
}

func TestTranslateChatSSEToMessagesIncludesReasoningAndRefusalDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-14","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"reasoning_text":"think-1"}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"refusal":"policy text"}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"content":"visible"}}]}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"stop"}]}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	text := string(respBytes)
	if !strings.Contains(text, `"type":"thinking_delta"`) {
		t.Fatalf("expected thinking_delta in translated stream, got %s", text)
	}
	if !strings.Contains(text, `"thinking":"think-1"`) {
		t.Fatalf("expected reasoning text mapped to thinking_delta, got %s", text)
	}
	if !strings.Contains(text, `"text":"policy text"`) {
		t.Fatalf("expected refusal mapped to text_delta, got %s", text)
	}
}

func TestTranslateChatSSEToMessagesProcessesAllChoicesInChunk(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-15","model":"gpt-4o","choices":[` +
			`{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"search","arguments":""}}]}},` +
			`{"index":1,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]}}]}`,
		"",
		`data: {"choices":[` +
			`{"index":1,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"beta\"}"}}]}},` +
			`{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"alpha\"}"}}]}}]}`,
		"",
		`data: {"choices":[{"index":1,"finish_reason":"function_call"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	text := string(respBytes)
	if !strings.Contains(text, `"id":"call_0"`) || !strings.Contains(text, `"id":"call_1"`) {
		t.Fatalf("expected tool calls from all choices, got %s", text)
	}
	if !strings.Contains(text, `\"q\":\"beta\"`) {
		t.Fatalf("expected tool delta from non-zero choice index, got %s", text)
	}
	if !strings.Contains(text, `"stop_reason":"tool_use"`) {
		t.Fatalf("expected function_call finish reason mapped to tool_use, got %s", text)
	}
}

func TestTranslateChatSSEToMessagesFinalizesOnDoneWithoutFinishReason(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-16","model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_text":"thinking"}}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
		"",
		sseDoneLine,
		"",
	}, "\n")

	translated := TranslateChatSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	respBytes, readErr := io.ReadAll(translated)
	if readErr != nil {
		t.Fatalf("read translated stream: %v", readErr)
	}
	text := string(respBytes)
	if !strings.Contains(text, `"type":"message_stop"`) {
		t.Fatalf("expected message_stop even when finish_reason missing, got %s", text)
	}
	if !strings.Contains(text, `"stop_reason":"end_turn"`) {
		t.Fatalf("expected default stop_reason end_turn on DONE-only stream, got %s", text)
	}
}

func decodeSSEDataEvents(t *testing.T, text string) []map[string]any {
	t.Helper()
	events := make([]map[string]any, 0, 16)
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == sseDoneToken {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("unmarshal SSE payload: %v (line=%q)", err, line)
		}
		events = append(events, payload)
	}
	return events
}

func mustContainInOrder(t *testing.T, events []map[string]any, markers ...string) {
	t.Helper()
	cursor := 0
	for _, marker := range markers {
		found := false
		for cursor < len(events) {
			if eventMatchesMarker(events[cursor], marker) {
				found = true
				cursor++
				break
			}
			cursor++
		}
		if !found {
			t.Fatalf("expected marker %q in order, events=%v", marker, events)
		}
	}
}

func eventMatchesMarker(event map[string]any, marker string) bool {
	parts := strings.Split(marker, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return false
		}
		key := kv[0]
		want := kv[1]
		switch key {
		case "type":
			got, _ := event["type"].(string)
			if got != want {
				return false
			}
		case "index":
			got, ok := event["index"].(float64)
			if !ok || int(got) != atoiSafe(want) {
				return false
			}
		case "block":
			block, _ := event["content_block"].(map[string]any)
			got, _ := block["type"].(string)
			if got != want {
				return false
			}
		case "delta":
			delta, _ := event["delta"].(map[string]any)
			got, _ := delta["type"].(string)
			if got != want {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func atoiSafe(v string) int {
	if v == "0" {
		return 0
	}
	if v == "1" {
		return 1
	}
	if v == "2" {
		return 2
	}
	if v == "3" {
		return 3
	}
	if v == "4" {
		return 4
	}
	if v == "5" {
		return 5
	}
	return -1
}

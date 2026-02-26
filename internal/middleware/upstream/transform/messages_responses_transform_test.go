package transform

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestMessagesToResponsesRequestMaxOutputTokensThreshold(t *testing.T) {
	cases := []struct {
		name           string
		reqBody        string
		expectPresent  bool
		expectedTokens float64
	}{
		{
			name:          "missing max_tokens",
			reqBody:       `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
			expectPresent: false,
		},
		{
			name:          "below minimum one",
			reqBody:       `{"model":"gpt-4o","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
			expectPresent: false,
		},
		{
			name:          "below minimum fifteen",
			reqBody:       `{"model":"gpt-4o","max_tokens":15,"messages":[{"role":"user","content":"hi"}]}`,
			expectPresent: false,
		},
		{
			name:           "minimum sixteen",
			reqBody:        `{"model":"gpt-4o","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`,
			expectPresent:  true,
			expectedTokens: 16,
		},
		{
			name:           "normal value",
			reqBody:        `{"model":"gpt-4o","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
			expectPresent:  true,
			expectedTokens: 4096,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			converted, ok := MessagesToResponsesRequest([]byte(tc.reqBody))
			if !ok {
				t.Fatalf("MessagesToResponsesRequest failed")
			}

			var parsed map[string]any
			if err := json.Unmarshal(converted, &parsed); err != nil {
				t.Fatalf("unmarshal converted: %v", err)
			}

			value, exists := parsed["max_output_tokens"]
			if exists != tc.expectPresent {
				t.Fatalf("expected max_output_tokens present=%v, got %v body=%s", tc.expectPresent, exists, string(converted))
			}
			if tc.expectPresent {
				got, ok := value.(float64)
				if !ok {
					t.Fatalf("expected numeric max_output_tokens, got %T", value)
				}
				if got != tc.expectedTokens {
					t.Fatalf("expected max_output_tokens=%v, got %v", tc.expectedTokens, got)
				}
			}
		})
	}
}

func TestResponsesToMessagesResponsePreservesReasoningAsThinkingBlock(t *testing.T) {
	respBody := `{"id":"resp_1","model":"gpt-4o","output":[` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"first reason"},{"type":"summary_text","text":"second reason"}]},` +
		`{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"q\":\"hi\"}"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}` +
		`]}`

	translated, ok := ResponsesToMessagesResponse([]byte(respBody))
	if !ok {
		t.Fatalf("ResponsesToMessagesResponse failed")
	}

	var parsed struct {
		Content []struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
			Text     string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(translated, &parsed); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if len(parsed.Content) < 3 {
		t.Fatalf("expected thinking + tool_use + text blocks, got %s", string(translated))
	}
	if parsed.Content[0].Type != "thinking" {
		t.Fatalf("expected first block type thinking, got %q", parsed.Content[0].Type)
	}
	if !strings.Contains(parsed.Content[0].Thinking, "first reason") || !strings.Contains(parsed.Content[0].Thinking, "second reason") {
		t.Fatalf("expected merged reasoning text in thinking block, got %q", parsed.Content[0].Thinking)
	}
}

func TestTranslateResponsesSSEToMessagesIncludesThinkingDeltas(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-4o","status":"in_progress"}}`,
		"",
		`data: {"type":"response.reasoning_text.delta","delta":"thinking-1"}`,
		"",
		`data: {"type":"response.reasoning_text.delta","delta":"thinking-2"}`,
		"",
		`data: {"type":"response.reasoning_text.done","text":"thinking-1thinking-2"}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	translated := TranslateResponsesSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	data, err := io.ReadAll(translated)
	if err != nil {
		t.Fatalf("read translated stream: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"type":"thinking"`) {
		t.Fatalf("expected thinking block in stream, got %s", text)
	}
	if !strings.Contains(text, `"type":"thinking_delta"`) {
		t.Fatalf("expected thinking_delta in stream, got %s", text)
	}
	if !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected message_stop event in stream, got %s", text)
	}
}

func TestTranslateResponsesSSEToMessagesUsesMessageItemDoneText(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_2","model":"gpt-5-mini","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"isNewTopic\": false, \"title\": null}"}]}}`,
		"",
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	translated := TranslateResponsesSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	data, err := io.ReadAll(translated)
	if err != nil {
		t.Fatalf("read translated stream: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `\"isNewTopic\": false`) {
		t.Fatalf("expected message item done text in stream, got %s", text)
	}
	if !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected message_stop event in stream, got %s", text)
	}
}

func TestTranslateResponsesSSEToMessagesFinalizesOnDoneWithoutCompleted(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_3","model":"gpt-5-mini","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	translated := TranslateResponsesSSEToMessages(io.NopCloser(strings.NewReader(stream)))
	defer func() { _ = translated.Close() }()

	data, err := io.ReadAll(translated)
	if err != nil {
		t.Fatalf("read translated stream: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"text":"hello"`) {
		t.Fatalf("expected text delta in stream, got %s", text)
	}
	if !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected message_stop event when stream ends with [DONE], got %s", text)
	}
}

func TestMessagesToResponsesRequestInjectsAssistantThinkingBlocks(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"private context"},` +
		`{"type":"text","text":"visible text"}]}]}`

	converted, ok := MessagesToResponsesRequest([]byte(reqBody))
	if !ok {
		t.Fatalf("MessagesToResponsesRequest failed")
	}

	var parsed map[string]any
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}

	input, ok := parsed["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected one input message, got %#v", parsed["input"])
	}
	msg, _ := input[0].(map[string]any)
	content, _ := msg["content"].([]any)
	if len(content) < 2 {
		t.Fatalf("expected thinking injected as text content, got %#v", msg["content"])
	}

	var foundThinkingText bool
	var foundVisibleText bool
	for _, item := range content {
		block, _ := item.(map[string]any)
		blockType, _ := block["type"].(string)
		text, _ := block["text"].(string)
		if blockType == responsesTypeOutputText && strings.Contains(text, "private context") {
			foundThinkingText = true
		}
		if blockType == responsesTypeOutputText && strings.Contains(text, "visible text") {
			foundVisibleText = true
		}
	}
	if !foundThinkingText || !foundVisibleText {
		t.Fatalf("expected assistant thinking + visible text preserved in responses input, got %#v", msg["content"])
	}
}

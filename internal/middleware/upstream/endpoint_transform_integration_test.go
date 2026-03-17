package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/runtime/config"
	"copilot-proxy/internal/runtime/endpoint/transform"
	protocolmessages "copilot-proxy/internal/runtime/protocol/messages"
)

const (
	testMessagesHiBody  = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	testResponsesHiBody = `{"model":"gpt-4o","input":[{"role":"user","content":"hi"}]}`
	testToolNameSearch  = "search"
)

// These tests validate upstream-level codec wiring with transform.ApplyEndpointTransform.
func TestEndpointTransformNoopWhenSameEndpoint(t *testing.T) {
	body := testMessagesHiBody
	req := httptestRequestWithRC(t, config.ChatCompletionsPath, body, &middleware.RequestContext{
		SourceLocalPath:    config.ChatCompletionsPath,
		LocalPath:          config.ChatCompletionsPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var gotReqBody string
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		gotReqBody = string(data)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if gotReqBody != body {
		t.Fatalf("expected request body unchanged, got %q", gotReqBody)
	}
}

func TestEndpointTransformRejectsUnsupportedResponsesToChatConversion(t *testing.T) {
	reqBody := testResponsesHiBody
	req := httptestRequestWithRC(t, config.ResponsesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	called := false
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if called {
		t.Fatalf("next should not be called for unsupported conversion")
	}
	if resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected strict failure 502, got %#v", resp)
	}
}

func TestEndpointTransformMessagesToChatSSEConvertsBack(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
		Info:               middleware.RequestInfo{MappedModel: "gpt-4o"},
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	stream := strings.Join([]string{
		`data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		"",
		`data: {"choices":[{"index":0,"delta":{"content":"Hi"}}]}`,
		"",
		`data: {"choices":[{"index":0,"finish_reason":"stop"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, "event: message_start") {
		t.Fatalf("expected anthropic SSE stream, got %s", text)
	}
}

func TestEndpointTransformMessagesFromChatResponseMapsContentFilterToEndTurn(t *testing.T) {
	reqBody := testMessagesHiBody
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
		Info:               middleware.RequestInfo{MappedModel: "gpt-4o"},
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	upstreamResp := `{"id":"chatcmpl-cf","model":"gpt-4o","choices":[` +
		`{"index":0,"message":{"role":"assistant","content":"filtered"},` +
		`"finish_reason":"content_filter"}]}`
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(upstreamResp)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("expected anthropic json response, got %s", string(body))
	}
	if parsed["stop_reason"] != "end_turn" {
		t.Fatalf("expected stop_reason end_turn, got %v", parsed["stop_reason"])
	}
}

func TestEndpointTransformMessagesFromChatResponseAllowsEmptyChoices(t *testing.T) {
	reqBody := testMessagesHiBody
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
		Info:               middleware.RequestInfo{MappedModel: "gpt-4o"},
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	upstreamResp := `{"id":"chatcmpl-empty","model":"gpt-4o","choices":[]}`
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(upstreamResp)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	var parsed struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("expected anthropic json response, got %s", string(body))
	}
	if len(parsed.Content) != 0 {
		t.Fatalf("expected empty content for empty choices, got %v", parsed.Content)
	}
}

func TestEndpointTransformStrictFailureOnRequestConversionError(t *testing.T) {
	req := httptestRequestWithRC(t, config.ResponsesPath, "not-json", &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	called := false
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if called {
		t.Fatalf("next should not be called on request conversion failure")
	}
	if resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected strict failure 502, got %#v", resp)
	}
}

func TestEndpointTransformStrictFailureOnUnsupportedConversionEvenWithValidBody(t *testing.T) {
	reqBody := testResponsesHiBody
	req := httptestRequestWithRC(t, config.ResponsesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamChatCompletionsPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	called := false
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if called {
		t.Fatalf("next should not be called for unsupported conversion")
	}
	if resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected strict failure 502, got %#v", resp)
	}
}

func TestEndpointTransformMessagesToResponsesNormalizesLongUser(t *testing.T) {
	longUserID := "user_" + strings.Repeat("x", 120)
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],` +
		`"metadata":{"user_id":"` + longUserID + `"}}`

	capturedUser := func() string {
		req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
			SourceLocalPath:    config.MessagesPath,
			LocalPath:          config.MessagesPath,
			TargetUpstreamPath: config.UpstreamResponsesPath,
		})
		rc, _ := middleware.RequestContextFrom(req.Context())

		var upstreamReqBody []byte
		resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
			upstreamReqBody, _ = io.ReadAll(r.Body)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    r,
			}, nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var parsed map[string]any
		if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
			t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
		}
		user, _ := parsed["user"].(string)
		if user == "" {
			t.Fatalf("expected user in converted request, got %s", string(upstreamReqBody))
		}
		if len(user) > 64 {
			t.Fatalf("expected user length <= 64, got %d (%q)", len(user), user)
		}
		return user
	}

	first := capturedUser()
	second := capturedUser()
	if first != second {
		t.Fatalf("expected deterministic normalized user, got %q and %q", first, second)
	}
}

func TestEndpointTransformMessagesToResponsesNormalizesContentTypes(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"hello"},` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed struct {
		Input []struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	if len(parsed.Input) == 0 || len(parsed.Input[0].Content) < 2 {
		t.Fatalf("expected converted content blocks, got %s", string(upstreamReqBody))
	}
	if parsed.Input[0].Content[0].Type != "input_text" {
		t.Fatalf("expected first block type input_text, got %q", parsed.Input[0].Content[0].Type)
	}
	if parsed.Input[0].Content[1].Type != "input_image" {
		t.Fatalf("expected second block type input_image, got %q", parsed.Input[0].Content[1].Type)
	}
}

func TestEndpointTransformMessagesToResponsesAssistantTextUsesOutputText(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"assistant","content":[{"type":"text","text":"hello"}]}]}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed struct {
		Input []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	if len(parsed.Input) != 1 || parsed.Input[0].Role != "assistant" || len(parsed.Input[0].Content) != 1 {
		t.Fatalf("expected one assistant message content block, got %s", string(upstreamReqBody))
	}
	if parsed.Input[0].Content[0].Type != "output_text" {
		t.Fatalf("expected assistant text block type output_text, got %q", parsed.Input[0].Content[0].Type)
	}
}

func TestEndpointTransformMessagesToResponsesAssistantInputTextBlockIsNormalized(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"Assistant","content":[{"type":"input_text","text":"hello"}]}]}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed struct {
		Input []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	if len(parsed.Input) != 1 || len(parsed.Input[0].Content) != 1 {
		t.Fatalf("expected one converted input message, got %s", string(upstreamReqBody))
	}
	if parsed.Input[0].Role != "assistant" {
		t.Fatalf("expected assistant role normalized to lowercase, got %q", parsed.Input[0].Role)
	}
	if parsed.Input[0].Content[0].Type != "output_text" {
		t.Fatalf("expected assistant input_text block normalized to output_text, got %q", parsed.Input[0].Content[0].Type)
	}
}

func TestEndpointTransformMessagesToResponsesDirectMapsToolUseAndToolResult(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"search","input":{"q":"hi"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"ok"}]}` +
		`]}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	if _, hasMessages := parsed["messages"]; hasMessages {
		t.Fatalf("expected no chat messages field in direct responses request, got %v", parsed["messages"])
	}
	input, ok := parsed["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("expected responses input array, got %v", parsed["input"])
	}
	hasFunctionCall := false
	hasFunctionCallOutput := false
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch itemMap["type"] {
		case "function_call":
			hasFunctionCall = true
		case "function_call_output":
			hasFunctionCallOutput = true
		}
		if _, hasToolCalls := itemMap["tool_calls"]; hasToolCalls {
			t.Fatalf("expected direct responses shape without chat tool_calls, got %v", itemMap)
		}
	}
	if !hasFunctionCall {
		t.Fatalf("expected function_call item in responses input, got %s", string(upstreamReqBody))
	}
	if !hasFunctionCallOutput {
		t.Fatalf("expected function_call_output item in responses input, got %s", string(upstreamReqBody))
	}
}

func TestEndpointTransformMessagesToResponsesNormalizesToolsShape(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"name":"search","description":"query","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],` +
		`"tool_choice":{"type":"tool","name":"search"}}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	tools, ok := parsed["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools array, got %v", parsed["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("expected tool object, got %T", tools[0])
	}
	if tool["name"] != testToolNameSearch {
		t.Fatalf("expected responses tool name=%s, got %v", testToolNameSearch, tool["name"])
	}
	if _, hasFn := tool["function"]; hasFn {
		t.Fatalf("expected responses tool shape without nested function, got %v", tool)
	}

	choice, ok := parsed["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected tool_choice object, got %T", parsed["tool_choice"])
	}
	if choice["type"] != "function" || choice["name"] != testToolNameSearch {
		t.Fatalf("expected responses tool_choice function+name, got %v", choice)
	}
	if _, hasFn := choice["function"]; hasFn {
		t.Fatalf("expected responses tool_choice shape without nested function, got %v", choice)
	}
}

func TestEndpointTransformMessagesToResponsesBuildsReasoningFromEffort(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"high"}}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var upstreamReqBody []byte
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		upstreamReqBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
	}
	reasoning, ok := parsed["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning object, got %T", parsed["reasoning"])
	}
	if reasoning["summary"] != "auto" {
		t.Fatalf("expected reasoning.summary=auto, got %v", reasoning["summary"])
	}
	if reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning.effort=high, got %v", reasoning["effort"])
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("expected reasoning_effort removed, got %v", parsed["reasoning_effort"])
	}
}

func TestEndpointTransformMessagesFromResponsesResponseDirectMapsFunctionCallToToolUse(t *testing.T) {
	reqBody := testMessagesHiBody
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	upstreamResp := `{"id":"resp_1","model":"gpt-4o","status":"completed","output":[` +
		`{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"q\":\"hi\"}"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}` +
		`]}`
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(upstreamResp)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	var parsed struct {
		Content []struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
			Text  string         `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		t.Fatalf("expected messages-style json response, got %s", string(bodyBytes))
	}
	if len(parsed.Content) < 2 {
		t.Fatalf("expected tool_use + text blocks, got %s", string(bodyBytes))
	}
	if parsed.Content[0].Type != "tool_use" {
		t.Fatalf("expected first block tool_use, got %q", parsed.Content[0].Type)
	}
	if parsed.Content[0].ID != "call_1" || parsed.Content[0].Name != testToolNameSearch {
		t.Fatalf("expected tool_use id/name from function_call, got %+v", parsed.Content[0])
	}
	if parsed.Content[0].Input["q"] != "hi" {
		t.Fatalf("expected tool_use input q=hi, got %+v", parsed.Content[0].Input)
	}
	if parsed.Content[1].Type != "text" || parsed.Content[1].Text != "done" {
		t.Fatalf("expected trailing text block \"done\", got %+v", parsed.Content[1])
	}
}

func TestEndpointTransformMessagesFromResponsesSSEDirectIncludesToolAndTextDeltas(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.MessagesPath,
		LocalPath:          config.MessagesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
		Info:               middleware.RequestInfo{MappedModel: "gpt-4o"},
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	stream := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-4o","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"search"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"q\":\"hi\"}"}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	text := string(bodyBytes)
	if !strings.Contains(text, `"type":"input_json_delta"`) {
		t.Fatalf("expected input_json_delta in messages SSE, got %s", text)
	}
	if !strings.Contains(text, `"type":"text_delta"`) {
		t.Fatalf("expected text_delta in messages SSE, got %s", text)
	}
	if !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected message_stop event, got %s", text)
	}
}

func TestEndpointTransformMessagesToResponsesMapsEffortAliases(t *testing.T) {
	cases := []struct {
		name       string
		rawEffort  string
		wantEffort string
	}{
		{name: "max->high", rawEffort: "max", wantEffort: "high"},
		{name: "minimal->low", rawEffort: "minimal", wantEffort: "low"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],` +
				`"output_config":{"effort":"` + tc.rawEffort + `"}}`
			req := httptestRequestWithRC(t, config.MessagesPath, reqBody, &middleware.RequestContext{
				SourceLocalPath:    config.MessagesPath,
				LocalPath:          config.MessagesPath,
				TargetUpstreamPath: config.UpstreamResponsesPath,
			})
			rc, _ := middleware.RequestContextFrom(req.Context())

			var upstreamReqBody []byte
			resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
				upstreamReqBody, _ = io.ReadAll(r.Body)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
					Request:    r,
				}, nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var parsed map[string]any
			if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
				t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
			}
			reasoning, ok := parsed["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("expected reasoning object, got %T", parsed["reasoning"])
			}
			if reasoning["effort"] != tc.wantEffort {
				t.Fatalf("expected reasoning.effort=%s, got %v", tc.wantEffort, reasoning["effort"])
			}
		})
	}
}

func TestEndpointTransformMessagesToResponsesSkipsReasoningWhenEffortMissingOrInvalid(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing effort",
			body: testMessagesHiBody,
		},
		{
			name: "invalid effort",
			body: `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"ultra"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptestRequestWithRC(t, config.MessagesPath, tc.body, &middleware.RequestContext{
				SourceLocalPath:    config.MessagesPath,
				LocalPath:          config.MessagesPath,
				TargetUpstreamPath: config.UpstreamResponsesPath,
			})
			rc, _ := middleware.RequestContextFrom(req.Context())

			var upstreamReqBody []byte
			resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
				upstreamReqBody, _ = io.ReadAll(r.Body)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
					Request:    r,
				}, nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var parsed map[string]any
			if err := json.Unmarshal(upstreamReqBody, &parsed); err != nil {
				t.Fatalf("expected json upstream request, got %s", string(upstreamReqBody))
			}
			if _, ok := parsed["reasoning"]; ok {
				t.Fatalf("expected no reasoning when effort missing or invalid, got %v", parsed["reasoning"])
			}
			if _, ok := parsed["reasoning_effort"]; ok {
				t.Fatalf("expected no reasoning_effort when effort missing or invalid, got %v", parsed["reasoning_effort"])
			}
		})
	}
}

func TestEndpointTransformResponsesPassthroughKeepsBodyUntouched(t *testing.T) {
	body := `{"model":"gpt-4o","user":"` + strings.Repeat("u", 90) + `","input":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	req := httptestRequestWithRC(t, config.ResponsesPath, body, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var gotReqBody string
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		gotReqBody = string(data)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if gotReqBody != body {
		t.Fatalf("expected responses request body unchanged, got %q", gotReqBody)
	}
}

func TestEndpointTransformResponsesPassthroughKeepsSmallMaxOutputTokensUntouched(t *testing.T) {
	body := `{"model":"gpt-4o","max_output_tokens":1,"input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	req := httptestRequestWithRC(t, config.ResponsesPath, body, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	var gotReqBody string
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		gotReqBody = string(data)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotReqBody != body {
		t.Fatalf("expected responses request body unchanged, got %q", gotReqBody)
	}
}

func TestEndpointTransformRejectsUnsupportedChatToMessagesConversion(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stop":"END"}`
	req := httptestRequestWithRC(t, config.ChatCompletionsPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ChatCompletionsPath,
		LocalPath:          config.ChatCompletionsPath,
		TargetUpstreamPath: config.UpstreamMessagesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	called := false
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if called {
		t.Fatalf("next should not be called for unsupported conversion")
	}
	if resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected strict failure 502, got %#v", resp)
	}
}

func TestEndpointTransformRejectsUnsupportedChatToMessagesConversionWithStopArray(t *testing.T) {
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stop":["END","STOP"]}`
	req := httptestRequestWithRC(t, config.ChatCompletionsPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ChatCompletionsPath,
		LocalPath:          config.ChatCompletionsPath,
		TargetUpstreamPath: config.UpstreamMessagesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	called := false
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errUnexpectedNextCall
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closeResponse(resp)
	if called {
		t.Fatalf("next should not be called for unsupported conversion")
	}
	if resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected strict failure 502, got %#v", resp)
	}
}

func TestEndpointTransformPassesThrough4xxErrorResponseUnchanged(t *testing.T) {
	reqBody := testResponsesHiBody
	req := httptestRequestWithRC(t, config.ResponsesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	errorBody := `{"error":{"message":"invalid model","type":"invalid_request_error","code":"model_not_found"}}`
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(errorBody)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 pass-through, got %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != errorBody {
		t.Fatalf("expected error body unchanged, got %s", string(data))
	}
}

func TestEndpointTransformPassesThrough5xxErrorResponseUnchanged(t *testing.T) {
	reqBody := testResponsesHiBody
	req := httptestRequestWithRC(t, config.ResponsesPath, reqBody, &middleware.RequestContext{
		SourceLocalPath:    config.ResponsesPath,
		LocalPath:          config.ResponsesPath,
		TargetUpstreamPath: config.UpstreamResponsesPath,
	})
	rc, _ := middleware.RequestContextFrom(req.Context())

	errorBody := `{"error":{"message":"internal server error"}}`
	resp, err := transform.ApplyEndpointTransform(req, rc, testEndpointCodec(), func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(errorBody)),
			Request:    r,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 pass-through, got %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != errorBody {
		t.Fatalf("expected error body unchanged, got %s", string(data))
	}
}

func testEndpointCodec() transform.EndpointCodec {
	return transform.EndpointCodec{
		MessagesToChatRequest:       protocolmessages.MessagesToChatRequest,
		ChatToMessagesResponse:      protocolmessages.ChatToMessagesResponse,
		ChatSSEToMessages:           protocolmessages.TranslateChatSSEToMessages,
		MessagesToResponsesRequest:  protocolmessages.MessagesToResponsesRequest,
		ResponsesToMessagesResponse: protocolmessages.ResponsesToMessagesResponse,
		ResponsesSSEToMessages:      protocolmessages.TranslateResponsesSSEToMessages,
	}
}

func httptestRequestWithRC(t *testing.T, path, body string, rc *middleware.RequestContext) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://localhost"+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if rc == nil {
		return req
	}
	return req.WithContext(middleware.WithRequestContext(req.Context(), rc))
}

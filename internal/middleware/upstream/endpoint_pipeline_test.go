package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/middleware"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/reasoning"
)

const messagesStreamBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`

type stubCatalog struct {
	models []models.ModelInfo
}

func (s *stubCatalog) GetModels() []models.ModelInfo {
	copied := make([]models.ModelInfo, len(s.models))
	copy(copied, s.models)
	return copied
}

func TestEndpointPipelineKeepsResponsesOnSameEndpoint(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o"}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := `{"model":"gpt-4o","input":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.ResponsesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.ResponsesPath,
		LocalPath:       config.ResponsesPath,
		Info: middleware.RequestInfo{
			Model: "gpt-4o",
		},
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.URL.Path; got != config.UpstreamResponsesPath {
			t.Fatalf("expected upstream path %q, got %q", config.UpstreamResponsesPath, got)
		}
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if string(upstreamBody) != reqBody {
		t.Fatalf("expected upstream body unchanged, got %s", string(upstreamBody))
	}

	rc, ok := middleware.RequestContextFrom(ctx.Request.Context())
	if !ok || rc == nil {
		t.Fatalf("expected request context")
	}
	if rc.SourceLocalPath != config.ResponsesPath {
		t.Fatalf("expected source local path %q, got %q", config.ResponsesPath, rc.SourceLocalPath)
	}
	if rc.TargetUpstreamPath != config.UpstreamResponsesPath {
		t.Fatalf("expected target upstream path %q, got %q", config.UpstreamResponsesPath, rc.TargetUpstreamPath)
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("expected response passthrough, got %s", string(body))
	}
}

func TestEndpointPipelineRuntimeOptionsProviderUpdatesHaikuFallbackPerRequest(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{
		{ID: "gpt-5-mini", Endpoints: []string{config.UpstreamResponsesPath}},
		{ID: "grok-code-fast-1", Endpoints: []string{config.UpstreamResponsesPath}},
	}}
	fallbackModels := []string{"gpt-5-mini"}
	mw := NewMessagesTranslateWithRuntimeOptions(catalog, config.PathMapping, func() MessagesTranslateRuntimeOptions {
		return MessagesTranslateRuntimeOptions{
			ClaudeHaikuFallbackModels: fallbackModels,
			ReasoningPolicies:         nil,
		}
	})

	makeRequest := func() string {
		reqBody := `{"model":"claude-haiku-latest","input":[{"role":"user","content":"hello"}]}`
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			"http://localhost"+config.ResponsesPath,
			strings.NewReader(reqBody),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
			SourceLocalPath: config.ResponsesPath,
			LocalPath:       config.ResponsesPath,
		}))
		ctx := &middleware.Context{Request: req}

		var mappedModel string
		resp, err := mw.Handle(ctx, func() (*http.Response, error) {
			bodyBytes, readErr := io.ReadAll(ctx.Request.Body)
			if readErr != nil {
				t.Fatalf("read upstream body: %v", readErr)
			}
			var payload map[string]any
			if unmarshalErr := json.Unmarshal(bodyBytes, &payload); unmarshalErr != nil {
				t.Fatalf("unmarshal upstream body: %v", unmarshalErr)
			}
			modelValue, _ := payload["model"].(string)
			mappedModel = modelValue
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    ctx.Request,
			}, nil
		})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		closeResponse(resp)
		return mappedModel
	}

	if got := makeRequest(); got != "gpt-5-mini" {
		t.Fatalf("expected first request mapped to gpt-5-mini, got %q", got)
	}
	fallbackModels = []string{"grok-code-fast-1"}
	if got := makeRequest(); got != "grok-code-fast-1" {
		t.Fatalf("expected second request mapped to grok-code-fast-1, got %q", got)
	}
}

func TestEndpointPipelineRuntimeOptionsProviderUpdatesReasoningPoliciesPerRequest(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{
		{
			ID:                       "gpt-5-mini",
			Endpoints:                []string{config.UpstreamResponsesPath},
			SupportedReasoningEffort: []string{"low", "medium", "high"},
		},
	}}
	policies := []reasoning.Policy{
		{Model: "gpt-5-mini", Target: reasoning.TargetResponses, Effort: reasoning.EffortNone},
	}
	mw := NewMessagesTranslateWithRuntimeOptions(catalog, config.PathMapping, func() MessagesTranslateRuntimeOptions {
		return MessagesTranslateRuntimeOptions{
			ClaudeHaikuFallbackModels: nil,
			ReasoningPolicies:         policies,
		}
	})

	makeRequest := func() map[string]any {
		reqBody := `{"model":"gpt-5-mini","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			"http://localhost"+config.MessagesPath,
			strings.NewReader(reqBody),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
			SourceLocalPath: config.MessagesPath,
			LocalPath:       config.MessagesPath,
		}))
		ctx := &middleware.Context{Request: req}

		var payload map[string]any
		resp, err := mw.Handle(ctx, func() (*http.Response, error) {
			bodyBytes, readErr := io.ReadAll(ctx.Request.Body)
			if readErr != nil {
				t.Fatalf("read upstream body: %v", readErr)
			}
			if unmarshalErr := json.Unmarshal(bodyBytes, &payload); unmarshalErr != nil {
				t.Fatalf("unmarshal upstream body: %v", unmarshalErr)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Request:    ctx.Request,
			}, nil
		})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		closeResponse(resp)
		return payload
	}

	first := makeRequest()
	if _, exists := first["reasoning"]; exists {
		t.Fatalf("expected no reasoning field when policy effort is none, got %#v", first["reasoning"])
	}

	policies = []reasoning.Policy{
		{Model: "gpt-5-mini", Target: reasoning.TargetResponses, Effort: reasoning.EffortHigh},
	}
	second := makeRequest()
	reasoningField, ok := second["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning field after policy update, got %#v", second["reasoning"])
	}
	if got := reasoningField["effort"]; got != "high" {
		t.Fatalf("expected reasoning effort high, got %#v", got)
	}
}

func TestEndpointPipelineTransformsMessagesToChatJSONResponse(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o", Endpoints: []string{
		config.UpstreamChatCompletionsPath,
	}}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":5}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.URL.Path; got != config.UpstreamChatCompletionsPath {
			t.Fatalf("expected upstream path %q, got %q", config.UpstreamChatCompletionsPath, got)
		}
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		chatResp := `{"id":"chatcmpl-2","object":"chat.completion","model":"gpt-4o","choices":[` +
			`{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(chatResp)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var upstreamParsed map[string]any
	if err := json.Unmarshal(upstreamBody, &upstreamParsed); err != nil {
		t.Fatalf("unmarshal upstream request body: %v", err)
	}
	messages, ok := upstreamParsed["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one upstream message, got %v", upstreamParsed["messages"])
	}
	userMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("expected user message object, got %T", messages[0])
	}
	contentParts, ok := userMsg["content"].([]any)
	if !ok || len(contentParts) != 1 {
		t.Fatalf("expected one content part, got %#v", userMsg["content"])
	}
	firstPart, ok := contentParts[0].(map[string]any)
	if !ok || firstPart["type"] != "text" || firstPart["text"] != "hi" {
		t.Fatalf("expected text content part {type:text,text:hi}, got %#v", contentParts[0])
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("expected messages-style response json, got %s", string(body))
	}
	if parsed["type"] != "message" {
		t.Fatalf("expected anthropic message response, got %v", parsed["type"])
	}
}

func TestEndpointPipelineTransformsMessagesToChatSSEResponse(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o", Endpoints: []string{
		config.UpstreamChatCompletionsPath,
	}}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := messagesStreamBody
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

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

	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.URL.Path; got != config.UpstreamChatCompletionsPath {
			t.Fatalf("expected upstream path %q, got %q", config.UpstreamChatCompletionsPath, got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	respText := string(respBytes)
	if !strings.Contains(respText, "event: message_start") {
		t.Fatalf("expected message_start event, got %s", respText)
	}
	if !strings.Contains(respText, "event: content_block_delta") {
		t.Fatalf("expected content_block_delta event, got %s", respText)
	}
	if !strings.Contains(respText, "event: message_stop") {
		t.Fatalf("expected message_stop event, got %s", respText)
	}
}

func TestEndpointPipelineKeepsMessagesBodyWhenModelSupportsMessagesEndpoint(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "claude-3", Endpoints: []string{
		config.UpstreamMessagesPath,
	}}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := `{"model":"claude-3","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.URL.Path; got != config.UpstreamMessagesPath {
			t.Fatalf("expected upstream path %q, got %q", config.UpstreamMessagesPath, got)
		}
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`ok`)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !bytes.Equal(bytes.TrimSpace(upstreamBody), bytes.TrimSpace([]byte(reqBody))) {
		t.Fatalf("expected request body passthrough, got %s", string(upstreamBody))
	}
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}
	if string(respBody) != "ok" {
		t.Fatalf("expected passthrough response body, got %s", string(respBody))
	}
}

func TestEndpointPipelineRewritesModelForMessagesPassthrough(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{
		{ID: "gpt-5-mini", Endpoints: []string{config.UpstreamMessagesPath}},
	}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := `{"model":"claude-haiku-3.5","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		if got := ctx.Request.URL.Path; got != config.UpstreamMessagesPath {
			t.Fatalf("expected upstream path %q, got %q", config.UpstreamMessagesPath, got)
		}
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`ok`)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamBody, &parsed); err != nil {
		t.Fatalf("expected json body, got %s", string(upstreamBody))
	}
	if parsed["model"] != "gpt-5-mini" {
		t.Fatalf("expected rewritten model gpt-5-mini, got %v", parsed["model"])
	}
}

func TestEndpointPipelineAppliesReasoningPolicyWithModelSupport(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o", Endpoints: []string{
		config.UpstreamResponsesPath,
	}, SupportedReasoningEffort: []string{"low"}}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping, map[string]string{
		"gpt-4o@responses": "high",
	})

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamBody))
	}
	reasoningObj, ok := parsed["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning object, got %T", parsed["reasoning"])
	}
	if reasoningObj["effort"] != "low" {
		t.Fatalf("expected mapped effort low, got %v", reasoningObj["effort"])
	}
}

func TestEndpointPipelineSkipsReasoningWhenModelSupportMissing(t *testing.T) {
	catalog := &stubCatalog{models: []models.ModelInfo{{ID: "gpt-4o", Endpoints: []string{
		config.UpstreamChatCompletionsPath,
	}}}}
	mw := NewMessagesTranslate(catalog, nil, config.PathMapping)

	reqBody := `{"model":"gpt-4o","output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://localhost"+config.MessagesPath,
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req = req.WithContext(middleware.WithRequestContext(req.Context(), &middleware.RequestContext{
		SourceLocalPath: config.MessagesPath,
		LocalPath:       config.MessagesPath,
	}))
	ctx := &middleware.Context{Request: req}

	var upstreamBody []byte
	resp, err := mw.Handle(ctx, func() (*http.Response, error) {
		upstreamBody, _ = io.ReadAll(ctx.Request.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    ctx.Request,
		}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed map[string]any
	if err := json.Unmarshal(upstreamBody, &parsed); err != nil {
		t.Fatalf("expected json upstream request, got %s", string(upstreamBody))
	}
	if _, ok := parsed["reasoning_effort"]; ok {
		t.Fatalf("expected no reasoning_effort without model support, got %v", parsed["reasoning_effort"])
	}
}

func TestMessagesTranslate_UsesBuiltinReasoningPoliciesWhenNoConfig(t *testing.T) {
	mw := NewMessagesTranslate(&stubCatalog{}, nil, config.PathMapping)

	got, ok := reasoning.MatchPolicy(mw.reasoningPolicies, "gpt-5-mini", reasoning.TargetResponses)
	if !ok || got != reasoning.EffortNone {
		t.Fatalf("expected builtin gpt-5-mini@responses=none, got %q ok=%v", got, ok)
	}

	got, ok = reasoning.MatchPolicy(mw.reasoningPolicies, "grok-code-fast-1", reasoning.TargetChat)
	if !ok || got != reasoning.EffortNone {
		t.Fatalf("expected builtin grok-code-fast-1@chat=none, got %q ok=%v", got, ok)
	}
}

func TestMessagesTranslate_ConfigPolicyOverridesBuiltin(t *testing.T) {
	mw := NewMessagesTranslate(&stubCatalog{}, nil, config.PathMapping, map[string]string{
		"gpt-5-mini@responses": "high",
	})

	got, ok := reasoning.MatchPolicy(mw.reasoningPolicies, "gpt-5-mini", reasoning.TargetResponses)
	if !ok || got != reasoning.EffortHigh {
		t.Fatalf("expected override gpt-5-mini@responses=high, got %q ok=%v", got, ok)
	}
}

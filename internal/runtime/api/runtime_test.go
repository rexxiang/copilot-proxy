package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	runtimeconfig "copilot-proxy/internal/runtime/config"
)

func TestRuntimeExecuteMapsChatCompletionsPath(t *testing.T) {
	var upstreamPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer token-value" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
		ResolveToken: func(ctx context.Context, accountRef string) (string, error) {
			if accountRef != "acct-1" {
				t.Fatalf("unexpected account ref: %s", accountRef)
			}
			return "token-value", nil
		},
	})

	request := RequestInvocation{
		Method: http.MethodPost,
		Path:   runtimeconfig.ChatCompletionsPath,
		Header: map[string]string{
			"Content-Type":      "application/json",
			"X-Copilot-Account": "acct-1",
		},
		Body: []byte(`{"model":"gpt-4o"}`),
	}

	var (
		callbacks int
		status    int
		body      string
	)
	err := runtime.Execute(context.Background(), request, ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(result ExecuteResult) {
			callbacks++
			status = result.StatusCode
			body = string(result.Body)
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("expected 1 callback, got %d", callbacks)
	}
	if upstreamPath != runtimeconfig.UpstreamChatCompletionsPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
	if status != http.StatusOK || body != `{"result":"ok"}` {
		t.Fatalf("unexpected result status/body: %d, %s", status, body)
	}
}

func TestRuntimeExecuteMessagesFallbackToResponses(t *testing.T) {
	var upstreamPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
		ResolveModel: func(ctx context.Context, modelID string) (ModelInfo, error) {
			return ModelInfo{
				ID:        modelID,
				Endpoints: []string{runtimeconfig.UpstreamResponsesPath},
			}, nil
		},
	})

	request := RequestInvocation{
		Method: http.MethodPost,
		Path:   runtimeconfig.MessagesPath,
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"gpt-4o"}`),
	}

	err := runtime.Execute(context.Background(), request, ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(result ExecuteResult) {
			if result.Error != "" {
				t.Fatalf("unexpected callback error: %s", result.Error)
			}
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if upstreamPath != runtimeconfig.UpstreamResponsesPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
}

func TestRuntimeExecuteMessagesFallbackToResponsesTransformsRequestAndResponse(t *testing.T) {
	var (
		upstreamPath string
		upstreamBody []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_1","model":"gpt-4o","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}`))
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
		ResolveModel: func(ctx context.Context, modelID string) (ModelInfo, error) {
			return ModelInfo{
				ID:        modelID,
				Endpoints: []string{runtimeconfig.UpstreamResponsesPath},
			}, nil
		},
	})

	request := RequestInvocation{
		Method: http.MethodPost,
		Path:   runtimeconfig.MessagesPath,
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`),
	}

	var responseBody []byte
	err := runtime.Execute(context.Background(), request, ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(result ExecuteResult) {
			if result.Error != "" {
				t.Fatalf("unexpected callback error: %s", result.Error)
			}
			responseBody = append([]byte(nil), result.Body...)
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if upstreamPath != runtimeconfig.UpstreamResponsesPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}

	var upstreamPayload map[string]any
	if err := json.Unmarshal(upstreamBody, &upstreamPayload); err != nil {
		t.Fatalf("unmarshal upstream request: %v", err)
	}
	if _, ok := upstreamPayload["input"]; !ok {
		t.Fatalf("expected responses input payload, got %s", string(upstreamBody))
	}
	if _, ok := upstreamPayload["messages"]; ok {
		t.Fatalf("expected messages field to be removed for responses payload, got %s", string(upstreamBody))
	}

	var parsed struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		t.Fatalf("unmarshal transformed response: %v", err)
	}
	if parsed.Type != "message" {
		t.Fatalf("expected anthropic message response, got %q", parsed.Type)
	}
	if len(parsed.Content) != 1 || parsed.Content[0].Type != "text" || parsed.Content[0].Text != "done" {
		t.Fatalf("unexpected transformed content: %+v", parsed.Content)
	}
}

func TestRuntimeExecuteRewritesMappedModelInBody(t *testing.T) {
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
		ResolveModel: func(ctx context.Context, modelID string) (ModelInfo, error) {
			return ModelInfo{
				ID:        "claude-sonnet-4.5",
				Endpoints: []string{runtimeconfig.UpstreamChatCompletionsPath},
			}, nil
		},
	})

	request := RequestInvocation{
		Method: http.MethodPost,
		Path:   runtimeconfig.ChatCompletionsPath,
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hi"}]}`),
	}

	err := runtime.Execute(context.Background(), request, ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(result ExecuteResult) {
			if result.Error != "" {
				t.Fatalf("unexpected callback error: %s", result.Error)
			}
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(upstreamBody, &payload); err != nil {
		t.Fatalf("unmarshal upstream request: %v", err)
	}
	if got, _ := payload["model"].(string); got != "claude-sonnet-4.5" {
		t.Fatalf("expected rewritten model id, got %q in %s", got, string(upstreamBody))
	}
}

func TestRuntimeExecuteModelsPathPassesThroughRawResponse(t *testing.T) {
	var upstreamPath string
	rawBody := []byte("{\"data\":[{\"id\":\"gpt-4o\",\"billing\":{\"multiplier\":1.0}}],\"meta\":{\"raw\":true}}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rawBody)
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
	})

	request := RequestInvocation{
		Method: http.MethodGet,
		Path:   runtimeconfig.ModelsPath,
		Header: map[string]string{"Accept": "application/json"},
		Body:   nil,
	}

	var (
		callbacks int
		status    int
		body      []byte
	)
	err := runtime.Execute(context.Background(), request, ExecuteOptions{
		Mode: StreamModeCallback,
		ResultCallback: func(result ExecuteResult) {
			callbacks++
			status = result.StatusCode
			body = append([]byte(nil), result.Body...)
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("expected 1 callback, got %d", callbacks)
	}
	if upstreamPath != runtimeconfig.UpstreamModelsPath {
		t.Fatalf("expected upstream path %q, got %q", runtimeconfig.UpstreamModelsPath, upstreamPath)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if string(body) != string(rawBody) {
		t.Fatalf("expected raw body passthrough, got %s", string(body))
	}
}

func TestRuntimeExecuteModelsPathPreservesAllowlistedXHeadersAcrossCalls(t *testing.T) {
	type observedRequest struct {
		path            string
		apiVersion      string
		interactionType string
		interactionID   string
	}

	observed := make([]observedRequest, 0, 2)
	rawBody := []byte("{\"data\":[{\"id\":\"gpt-4o\",\"billing\":{\"multiplier\":1.0}}]}")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = append(observed, observedRequest{
			path:            r.URL.Path,
			apiVersion:      r.Header.Get("X-GitHub-Api-Version"),
			interactionType: r.Header.Get("X-Interaction-Type"),
			interactionID:   r.Header.Get("X-Interaction-Id"),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rawBody)
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
	})

	request := RequestInvocation{
		Method: http.MethodGet,
		Path:   runtimeconfig.ModelsPath,
		Header: map[string]string{
			"Accept":               "application/json",
			"X-GitHub-Api-Version": "2025-05-01",
			"X-Interaction-Type":   "conversation-agent",
			"X-Interaction-Id":     "interaction-1",
		},
	}
	for i := 0; i < 2; i++ {
		expectedInteractionID := "interaction-1"
		if i == 1 {
			expectedInteractionID = "interaction-2"
		}
		request.Header["X-Interaction-Id"] = expectedInteractionID

		var body []byte
		err := runtime.Execute(context.Background(), request, ExecuteOptions{
			Mode: StreamModeCallback,
			ResultCallback: func(result ExecuteResult) {
				body = append([]byte(nil), result.Body...)
			},
		})
		if err != nil {
			t.Fatalf("execute call %d: %v", i+1, err)
		}
		if string(body) != string(rawBody) {
			t.Fatalf("expected raw body passthrough on call %d, got %s", i+1, string(body))
		}
	}

	if len(observed) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(observed))
	}
	for i, got := range observed {
		expectedInteractionID := "interaction-1"
		if i == 1 {
			expectedInteractionID = "interaction-2"
		}
		if got.path != runtimeconfig.UpstreamModelsPath {
			t.Fatalf("call %d expected upstream path %q, got %q", i+1, runtimeconfig.UpstreamModelsPath, got.path)
		}
		if got.apiVersion != "2025-05-01" {
			t.Fatalf("call %d expected X-GitHub-Api-Version preserved, got %q", i+1, got.apiVersion)
		}
		if got.interactionType != "conversation-agent" {
			t.Fatalf("call %d expected X-Interaction-Type preserved, got %q", i+1, got.interactionType)
		}
		if got.interactionID != expectedInteractionID {
			t.Fatalf("call %d expected X-Interaction-Id %q, got %q", i+1, expectedInteractionID, got.interactionID)
		}
	}
}

func TestRuntimeFetchModelsUsesConfiguredBase(t *testing.T) {
	var (
		upstreamPath string
		authHeader   string
		customHeader string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		customHeader = r.Header.Get("X-Test-Header")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                   "gpt-4o",
					"name":                 "GPT-4o",
					"vendor":               "OpenAI",
					"version":              "1",
					"preview":              false,
					"model_picker_enabled": true,
					"supported_endpoints":  []string{runtimeconfig.UpstreamChatCompletionsPath},
					"billing": map[string]any{
						"is_premium": false,
						"multiplier": 1.0,
					},
					"capabilities": map[string]any{
						"family": "gpt-4o",
						"type":   "chat",
						"supports": map[string]any{
							"reasoning_effort": []string{"low"},
						},
						"limits": map[string]any{
							"max_context_window_tokens": 128000,
							"max_prompt_tokens":         128000,
							"max_output_tokens":         16384,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		SettingsProvider:  settingsProviderForTests(server.URL),
		HTTPClientFactory: func() *http.Client { return server.Client() },
	})

	items, err := runtime.FetchModels(context.Background(), "token-value")
	if err != nil {
		t.Fatalf("fetch models: %v", err)
	}
	if upstreamPath != runtimeconfig.UpstreamModelsPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
	if authHeader != "Bearer token-value" {
		t.Fatalf("unexpected authorization header: %s", authHeader)
	}
	if customHeader != "test-value" {
		t.Fatalf("unexpected custom header: %s", customHeader)
	}
	if len(items) != 1 || items[0].ID != "gpt-4o" {
		t.Fatalf("unexpected model items: %+v", items)
	}
}

func TestRuntimeFetchLoginUsesGitHubAPIBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "token token-value" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"alice"}`))
	}))
	defer server.Close()

	runtime := NewEngine(Options{
		HTTPClientFactory: func() *http.Client { return server.Client() },
		GitHubBaseURL:     server.URL,
	})

	login, err := runtime.FetchLogin(context.Background(), "token-value")
	if err != nil {
		t.Fatalf("fetch login: %v", err)
	}
	if login != "alice" {
		t.Fatalf("unexpected login: %s", login)
	}
}

func settingsProviderForTests(baseURL string) func(context.Context) (runtimeconfig.RuntimeSettings, error) {
	return func(context.Context) (runtimeconfig.RuntimeSettings, error) {
		settings := runtimeconfig.Default()
		settings.UpstreamBase = baseURL
		settings.RequiredHeaders = map[string]string{
			"X-Test-Header": "test-value",
		}
		return settings, nil
	}
}

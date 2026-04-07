//go:build cgo
// +build cgo

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	config "copilot-proxy/internal/runtime/config"
	core "copilot-proxy/internal/runtime/types"
)

func TestExecuteRequestNonStream(t *testing.T) {
	var upstreamPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	restore := withCAPISettings(server.URL, server.Client())
	defer restore()

	var callbacks int
	var status int
	var body string
	telemetryEvents := make([]string, 0, 3)

	opts := executeOptions{
		EventCallback: func(event eventEnvelope) {
			switch event.Kind {
			case "response_head":
				callbacks++
				status = payloadInt(event.Payload, "status_code")
				decoded, err := decodeEventBody(event.Payload)
				if err != nil {
					t.Fatalf("decode response body: %v", err)
				}
				body = string(decoded)
			case "telemetry":
				eventType, _ := event.Payload["type"].(string)
				telemetryEvents = append(telemetryEvents, eventType)
			case "fatal":
				t.Fatalf("unexpected fatal event: %#v", event.Payload)
			}
		},
	}
	deps := executeDeps{
		ResolveToken: func(ctx context.Context, accountRef string) (string, error) { return "token-value", nil },
		ResolveModel: func(ctx context.Context, modelID string) (modelInfo, error) { return modelInfo{ID: modelID}, nil },
	}

	request := core.RequestInvocation{
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"gpt-4o"}`),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	if err := executeRequest(context.Background(), string(payload), deps, opts); err != nil {
		t.Fatalf("executeRequest returned error: %v", err)
	}
	if callbacks != 1 {
		t.Fatalf("expected 1 callback, got %d", callbacks)
	}
	if status != http.StatusOK {
		t.Fatalf("unexpected status: %d", status)
	}
	if body != `{"result":"ok"}` {
		t.Fatalf("unexpected body: %s", body)
	}
	if upstreamPath != config.UpstreamChatCompletionsPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
	if len(telemetryEvents) != 3 || telemetryEvents[0] != "start" || telemetryEvents[1] != "first_byte" || telemetryEvents[2] != "end" {
		t.Fatalf("unexpected telemetry events: %v", telemetryEvents)
	}
}

func TestExecuteRequestStream(t *testing.T) {
	var upstreamPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("response writer does not support flushing")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: first\n\n"))
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)
		_, _ = w.Write([]byte("data: second\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	restore := withCAPISettings(server.URL, server.Client())
	defer restore()

	statuses := make([]int, 0, 2)
	bodyChunks := make([]string, 0, 2)
	opts := executeOptions{
		EventCallback: func(event eventEnvelope) {
			switch event.Kind {
			case "response_head":
				statuses = append(statuses, payloadInt(event.Payload, "status_code"))
				if decoded, err := decodeEventBody(event.Payload); err == nil && len(decoded) > 0 {
					bodyChunks = append(bodyChunks, string(decoded))
				} else if err != nil {
					t.Fatalf("decode response head body: %v", err)
				}
			case "response_chunk":
				statuses = append(statuses, 0)
				decoded, err := decodeEventBody(event.Payload)
				if err != nil {
					t.Fatalf("decode stream chunk body: %v", err)
				}
				if len(decoded) > 0 {
					bodyChunks = append(bodyChunks, string(decoded))
				}
			case "fatal":
				t.Fatalf("unexpected fatal event: %#v", event.Payload)
			}
		},
	}
	deps := executeDeps{
		ResolveToken: func(ctx context.Context, accountRef string) (string, error) { return "token-value", nil },
		ResolveModel: func(ctx context.Context, modelID string) (modelInfo, error) {
			return modelInfo{ID: modelID, Endpoints: []string{"/v1/messages"}}, nil
		},
	}

	request := core.RequestInvocation{
		Method: http.MethodPost,
		Path:   "/v1/messages",
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"gpt-4o"}`),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	if err := executeRequest(context.Background(), string(payload), deps, opts); err != nil {
		t.Fatalf("executeRequest returned error: %v", err)
	}
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 callbacks, got %d", len(statuses))
	}
	if statuses[0] != http.StatusOK {
		t.Fatalf("expected first callback status 200, got %d", statuses[0])
	}
	if statuses[1] != 0 {
		t.Fatalf("expected subsequent chunk status 0, got %d", statuses[1])
	}
	if len(bodyChunks) < 2 {
		t.Fatalf("expected stream chunks, got %v", bodyChunks)
	}
	if upstreamPath != config.UpstreamMessagesPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
}

func TestExecuteRequestMessagesFallbackToResponses(t *testing.T) {
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

	restore := withCAPISettings(server.URL, server.Client())
	defer restore()

	var responseBody []byte
	opts := executeOptions{
		EventCallback: func(event eventEnvelope) {
			switch event.Kind {
			case "response_head":
				decoded, err := decodeEventBody(event.Payload)
				if err != nil {
					t.Fatalf("decode response body: %v", err)
				}
				responseBody = append([]byte(nil), decoded...)
			case "fatal":
				t.Fatalf("unexpected fatal event: %#v", event.Payload)
			}
		},
	}
	deps := executeDeps{
		ResolveToken: func(ctx context.Context, accountRef string) (string, error) { return "token-value", nil },
		ResolveModel: func(ctx context.Context, modelID string) (modelInfo, error) {
			return modelInfo{ID: modelID, Endpoints: []string{config.UpstreamResponsesPath}}, nil
		},
	}

	request := core.RequestInvocation{
		Method: http.MethodPost,
		Path:   config.MessagesPath,
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   []byte(`{"model":"gpt-4o"}`),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	if err := executeRequest(context.Background(), string(payload), deps, opts); err != nil {
		t.Fatalf("executeRequest returned error: %v", err)
	}
	if upstreamPath != config.UpstreamResponsesPath {
		t.Fatalf("unexpected upstream path: %s", upstreamPath)
	}
	var upstreamPayload map[string]any
	if err := json.Unmarshal(upstreamBody, &upstreamPayload); err != nil {
		t.Fatalf("unmarshal upstream payload: %v", err)
	}
	if _, ok := upstreamPayload["input"]; !ok {
		t.Fatalf("expected responses input payload, got %s", string(upstreamBody))
	}
	if _, ok := upstreamPayload["messages"]; ok {
		t.Fatalf("expected converted request without messages field, got %s", string(upstreamBody))
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

func TestFetchHelperJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/copilot_internal/user":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"copilot_plan": "business"})
		case "/models":
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
						"supported_endpoints":  []string{"/chat/completions"},
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
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	restore := withCAPISettings(server.URL, server.Client())
	defer restore()
	githubAPIBase = server.URL

	userPayload, err := fetchUserInfoJSON(context.Background(), "token")
	if err != nil {
		t.Fatalf("fetchUserInfoJSON failed: %v", err)
	}
	if !strings.Contains(userPayload, "business") {
		t.Fatalf("unexpected user payload: %s", userPayload)
	}

	modelPayload, err := fetchModelsJSON(context.Background(), "token")
	if err != nil {
		t.Fatalf("fetchModelsJSON failed: %v", err)
	}
	if !strings.Contains(modelPayload, "gpt-4o") {
		t.Fatalf("unexpected models payload: %s", modelPayload)
	}
}

func withCAPISettings(baseURL string, client *http.Client) func() {
	previousClientMaker := httpClientMaker
	previousSettingsProvider := settingsProvider
	httpClientMaker = func() *http.Client { return client }
	settingsProvider = func() config.RuntimeSettings {
		settings := config.Default()
		settings.UpstreamBase = baseURL
		settings.RequiredHeaders = nil
		return settings
	}
	return func() {
		httpClientMaker = previousClientMaker
		settingsProvider = previousSettingsProvider
	}
}

func decodeEventBody(payload map[string]any) ([]byte, error) {
	bodyBase64, _ := payload["body_base64"].(string)
	if bodyBase64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(bodyBase64)
}

func payloadInt(payload map[string]any, key string) int {
	value, ok := payload[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

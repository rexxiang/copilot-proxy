package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchModels(t *testing.T) {
	modelsResponse := map[string]any{
		"data": []map[string]any{
			{
				"id":                   "gpt-4o",
				"name":                 "GPT-4o",
				"vendor":               "Azure OpenAI",
				"model_picker_enabled": true,
				"capabilities": map[string]any{
					"type":   "chat",
					"family": "gpt-4o",
					"supports": map[string]any{
						"reasoning_effort": []string{"low", "high", "medium"},
					},
					"limits": map[string]any{
						"max_context_window_tokens": 128000,
						"max_prompt_tokens":         128000,
						"max_output_tokens":         16000,
					},
				},
				"supported_endpoints": []string{"/chat/completions", "/responses"},
				"billing": map[string]any{
					"is_premium": false,
				},
			},
			{
				"id":                   "gpt-3.5-turbo",
				"name":                 "GPT-3.5 Turbo",
				"vendor":               "Azure OpenAI",
				"model_picker_enabled": false,
				"capabilities": map[string]any{
					"type":   "chat",
					"family": "gpt-3.5-turbo",
					"limits": map[string]any{
						"max_context_window_tokens": 16384,
						"max_prompt_tokens":         16000,
						"max_output_tokens":         4000,
					},
				},
				"billing": map[string]any{
					"is_premium": false,
				},
			},
			{
				"id":                   "claude-3.5-sonnet",
				"name":                 "Claude 3.5 Sonnet",
				"vendor":               "Anthropic",
				"model_picker_enabled": true,
				"capabilities": map[string]any{
					"type":   "chat",
					"family": "claude-sonnet",
					"supports": map[string]any{
						"reasoning_effort": []string{"high"},
					},
					"limits": map[string]any{
						"max_context_window_tokens": 144000,
						"max_prompt_tokens":         140000,
						"max_output_tokens":         10000,
					},
				},
				"supported_endpoints": []string{"/chat/completions", "/v1/messages"},
				"billing": map[string]any{
					"is_premium": true,
					"multiplier": 1.0,
				},
			},
			{
				"id":   "text-embedding-3-small",
				"name": "Embedding V3 small",
				"capabilities": map[string]any{
					"type": "embeddings",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("expected path /models, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header with test-token")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(modelsResponse); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := &http.Client{}
	items, err := FetchModels(context.Background(), client, server.URL, "test-token", nil)
	if err != nil {
		t.Fatalf("FetchModels failed: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 picker-enabled chat models, got %d", len(items))
	}

	gpt4o := findModel(items, "gpt-4o")
	if gpt4o == nil {
		t.Fatal("gpt-4o not found")
	}
	if !contains(gpt4o.Endpoints, "/chat/completions") {
		t.Error("expected gpt-4o to support /chat/completions")
	}
	if !contains(gpt4o.Endpoints, "/responses") {
		t.Error("expected gpt-4o to support /responses")
	}
	if gpt4o.IsPremium {
		t.Error("expected gpt-4o to be free")
	}
	if gpt4o.ContextWindow != 128000 {
		t.Errorf("expected gpt-4o context window 128000, got %d", gpt4o.ContextWindow)
	}
	if gpt4o.MaxPromptTokens != 128000 {
		t.Errorf("expected gpt-4o max prompt tokens 128000, got %d", gpt4o.MaxPromptTokens)
	}
	if gpt4o.MaxOutputTokens != 16000 {
		t.Errorf("expected gpt-4o max output tokens 16000, got %d", gpt4o.MaxOutputTokens)
	}
	if len(gpt4o.SupportedReasoningEffort) != 3 {
		t.Fatalf("expected three reasoning effort levels, got %#v", gpt4o.SupportedReasoningEffort)
	}
	if gpt4o.SupportedReasoningEffort[0] != "low" || gpt4o.SupportedReasoningEffort[1] != "medium" || gpt4o.SupportedReasoningEffort[2] != "high" {
		t.Fatalf("expected normalized reasoning levels [low medium high], got %#v", gpt4o.SupportedReasoningEffort)
	}

	claude := findModel(items, "claude-3.5-sonnet")
	if claude == nil {
		t.Fatal("claude-3.5-sonnet not found")
	}
	if !claude.IsPremium {
		t.Error("expected claude-3.5-sonnet to be premium")
	}
	if claude.Vendor != "Anthropic" {
		t.Errorf("expected vendor Anthropic, got %s", claude.Vendor)
	}
	if claude.ContextWindow != 144000 {
		t.Errorf("expected claude context window 144000, got %d", claude.ContextWindow)
	}
	if claude.MaxPromptTokens != 140000 {
		t.Errorf("expected claude max prompt tokens 140000, got %d", claude.MaxPromptTokens)
	}
	if claude.MaxOutputTokens != 10000 {
		t.Errorf("expected claude max output tokens 10000, got %d", claude.MaxOutputTokens)
	}
	if len(claude.SupportedReasoningEffort) != 1 || claude.SupportedReasoningEffort[0] != "high" {
		t.Fatalf("expected claude supported reasoning [high], got %#v", claude.SupportedReasoningEffort)
	}
}

func TestFetchModelsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &http.Client{}
	_, err := FetchModels(ctx, client, server.URL, "test-token", nil)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestFetchModelsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := &http.Client{}
	_, err := FetchModels(context.Background(), client, server.URL, "bad-token", nil)
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestFetchModelsViaDoerHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	client := &http.Client{}
	_, err := FetchModelsViaDoer(context.Background(), client, server.URL+"/copilot/models")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrModelsAPIFailed) {
		t.Fatalf("expected ErrModelsAPIFailed, got %v", err)
	}
	if !IsHTTPStatus(err, http.StatusMethodNotAllowed) {
		t.Fatalf("expected 405 status error, got %v", err)
	}
}

func TestFetchModelsViaDoerNilDoer(t *testing.T) {
	_, err := FetchModelsViaDoer(context.Background(), nil, "http://example.com/copilot/models")
	if err == nil {
		t.Fatal("expected nil doer error")
	}
}

func TestFetchModelsViaDoerDoesNotMutateDefaultManager(t *testing.T) {
	manager := DefaultModelsManager()
	original := manager.GetModels()
	manager.SetModels([]ModelInfo{{ID: "existing-model"}})
	t.Cleanup(func() {
		manager.SetModels(original)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                   "fetched-model",
					"name":                 "Fetched",
					"vendor":               "OpenAI",
					"model_picker_enabled": true,
					"capabilities": map[string]any{
						"type": "chat",
						"limits": map[string]any{
							"max_context_window_tokens": 16000,
							"max_prompt_tokens":         16000,
							"max_output_tokens":         4096,
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := &http.Client{}
	got, err := FetchModelsViaDoer(context.Background(), client, server.URL+"/copilot/models")
	if err != nil {
		t.Fatalf("fetch via doer: %v", err)
	}
	if len(got) != 1 || got[0].ID != "fetched-model" {
		t.Fatalf("unexpected fetched models: %+v", got)
	}

	current := manager.GetModels()
	if len(current) != 1 || current[0].ID != "existing-model" {
		t.Fatalf("expected default manager unchanged, got %+v", current)
	}
}

func findModel(items []ModelInfo, id string) *ModelInfo {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

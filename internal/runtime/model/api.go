package model

import (
	"context"
	"copilot-proxy/internal/runtime/reasoning"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	protocolpaths "copilot-proxy/internal/runtime/protocol/paths"

	"github.com/google/uuid"
)

var (
	ErrModelsAPIFailed = errors.New("models API returned non-200")
	errModelsDoerNil   = errors.New("models request doer is nil")
)

// RequestDoer performs HTTP requests.
type RequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPStatusError captures a non-2xx HTTP status together with a sentinel cause.
type HTTPStatusError struct {
	Err        error
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%v: %d", e.Err, e.StatusCode)
}

func (e *HTTPStatusError) Unwrap() error {
	return e.Err
}

// IsHTTPStatus returns true when err carries the specified HTTP status code.
func IsHTTPStatus(err error, code int) bool {
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode == code
}

// APIHeaders returns headers required for the /models API call.
func APIHeaders() map[string]string {
	return map[string]string{
		"Accept":               "application/json",
		"Content-Type":         "application/json",
		"X-GitHub-Api-Version": "2025-05-01",
		"X-Initiator":          "user",
		"X-Interaction-Type":   "conversation-agent",
		"Openai-Intent":        "conversation-agent",
	}
}

// responsePayload represents the /models API response.
type responsePayload struct {
	Data []modelData `json:"data"`
}

type modelData struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Vendor             string   `json:"vendor"`
	Version            string   `json:"version"`
	Preview            bool     `json:"preview"`
	ModelPickerEnabled bool     `json:"model_picker_enabled"`
	SupportedEndpoints []string `json:"supported_endpoints"`
	Billing            struct {
		IsPremium  bool    `json:"is_premium"`
		Multiplier float64 `json:"multiplier"`
	} `json:"billing"`
	Capabilities struct {
		Family   string `json:"family"`
		Type     string `json:"type"`
		Supports struct {
			ReasoningEffort []string `json:"reasoning_effort"`
		} `json:"supports"`
		Limits struct {
			MaxContextWindowTokens int `json:"max_context_window_tokens"`
			MaxPromptTokens        int `json:"max_prompt_tokens"`
			MaxOutputTokens        int `json:"max_output_tokens"`
		} `json:"limits"`
	} `json:"capabilities"`
}

// FetchModels retrieves available models directly from the Copilot API.
func FetchModels(ctx context.Context, client *http.Client, baseURL, token string, baseHeaders map[string]string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for key, value := range baseHeaders {
		req.Header.Set(key, value)
	}
	for key, value := range APIHeaders() {
		req.Header.Set(key, value)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Interaction-Id", uuid.New().String())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrModelsAPIFailed, resp.StatusCode)
	}

	return parseResponse(resp)
}

// FetchViaProxy retrieves available models through the local proxy.
func FetchViaProxy(ctx context.Context, client *http.Client, proxyURL string) ([]ModelInfo, error) {
	return FetchViaDoer(ctx, client, proxyURL+protocolpaths.ModelsPath)
}

// FetchViaDoer retrieves available models through any HTTP doer.
func FetchViaDoer(ctx context.Context, doer RequestDoer, requestURL string) ([]ModelInfo, error) {
	if doer == nil {
		return nil, errModelsDoerNil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for key, value := range APIHeaders() {
		req.Header.Set(key, value)
	}
	req.Header.Set("X-Interaction-Id", uuid.New().String())

	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models via doer: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{Err: ErrModelsAPIFailed, StatusCode: resp.StatusCode}
	}

	items, err := parseResponse(resp)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// FetchModelsViaDoer is kept for compatibility and delegates to FetchViaDoer.
func FetchModelsViaDoer(ctx context.Context, doer RequestDoer, requestURL string) ([]ModelInfo, error) {
	return FetchViaDoer(ctx, doer, requestURL)
}

func parseResponse(resp *http.Response) ([]ModelInfo, error) {
	var result responsePayload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]ModelInfo, 0, len(result.Data))
	for i := range result.Data {
		m := result.Data[i]
		if m.Capabilities.Type != "chat" || !m.ModelPickerEnabled {
			continue
		}

		endpoints := m.SupportedEndpoints
		if len(endpoints) == 0 {
			endpoints = []string{"/chat/completions"}
		}

		items = append(items, ModelInfo{
			ID:                       m.ID,
			Name:                     m.Name,
			Vendor:                   m.Vendor,
			Endpoints:                endpoints,
			SupportedReasoningEffort: reasoning.NormalizeSupportedEfforts(m.Capabilities.Supports.ReasoningEffort),
			IsPremium:                m.Billing.IsPremium,
			Multiplier:               m.Billing.Multiplier,
			Preview:                  m.Preview,
			Family:                   m.Capabilities.Family,
			ContextWindow:            m.Capabilities.Limits.MaxContextWindowTokens,
			MaxPromptTokens:          m.Capabilities.Limits.MaxPromptTokens,
			MaxOutputTokens:          m.Capabilities.Limits.MaxOutputTokens,
		})
	}

	return items, nil
}

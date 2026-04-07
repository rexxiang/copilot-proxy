//go:build integration
// +build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	runtimeconfig "copilot-proxy/internal/runtime/config"
	models "copilot-proxy/internal/runtime/model"
)

const (
	liveFlagEnv       = "COPILOT_LIVE_TEST"
	liveTokenEnv      = "COPILOT_TEST_GH_TOKEN"
	liveExpectedLogin = "COPILOT_TEST_EXPECT_LOGIN"
)

type liveCredentials struct {
	token string
}

func TestLiveAccount_RequestCode(t *testing.T) {
	requireLiveEnabled(t)
	engine := newLiveEngine("", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	device, err := engine.RequestCode(ctx)
	if err != nil {
		t.Fatalf("request code: %v", err)
	}
	if strings.TrimSpace(device.DeviceCode) == "" {
		t.Fatalf("expected non-empty device_code")
	}
	if strings.TrimSpace(device.UserCode) == "" {
		t.Fatalf("expected non-empty user_code")
	}
	if strings.TrimSpace(device.VerificationURI) == "" {
		t.Fatalf("expected non-empty verification_uri")
	}

	verificationURL, err := url.Parse(device.VerificationURI)
	if err != nil {
		t.Fatalf("parse verification_uri: %v", err)
	}
	if !strings.EqualFold(verificationURL.Host, "github.com") {
		t.Fatalf("expected verification host github.com, got %q", verificationURL.Host)
	}
	if !strings.HasPrefix(verificationURL.Path, "/login/device") {
		t.Fatalf("expected verification path prefix /login/device, got %q", verificationURL.Path)
	}
}

func TestLiveAccount_FetchLogin(t *testing.T) {
	creds := requireLiveCredentials(t)
	engine := newLiveEngine(creds.token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	login, err := engine.FetchLogin(ctx, creds.token)
	if err != nil {
		t.Fatalf("fetch login: %v", err)
	}
	if strings.TrimSpace(login) == "" {
		t.Fatalf("expected non-empty login")
	}

	expected := strings.TrimSpace(os.Getenv(liveExpectedLogin))
	if expected != "" && login != expected {
		t.Fatalf("expected login %q, got %q", expected, login)
	}
}

func TestLiveAccount_FetchUserInfo(t *testing.T) {
	creds := requireLiveCredentials(t)
	engine := newLiveEngine(creds.token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := engine.FetchUserInfo(ctx, creds.token)
	if err != nil {
		t.Fatalf("fetch user info: %v", err)
	}
	if info == nil {
		t.Fatalf("expected non-nil user info")
	}
}

func TestLiveModels_FetchModels(t *testing.T) {
	creds := requireLiveCredentials(t)
	engine := newLiveEngine(creds.token, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	items, err := engine.FetchModels(ctx, creds.token)
	if err != nil {
		t.Fatalf("fetch models: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected at least one model from /models")
	}
}

func TestLiveSession_Messages_ClaudeHaiku45(t *testing.T) {
	creds := requireLiveCredentials(t)
	modelIndex := fetchLiveModelIndex(t, creds.token)
	requireModelWithEndpoint(t, modelIndex, "claude-haiku-4.5", runtimeconfig.UpstreamMessagesPath)
	engine := newLiveEngine(creds.token, modelResolver(modelIndex))

	body := []byte(`{"model":"claude-haiku-4.5","max_tokens":1,"messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}`)
	status, responseBody := executeLiveRequest(t, engine, runtimeconfig.MessagesPath, body)
	assertLiveSuccessStatusAndBody(t, status, responseBody)
}

func TestLiveSession_ChatCompletions_GPT5Mini(t *testing.T) {
	creds := requireLiveCredentials(t)
	modelIndex := fetchLiveModelIndex(t, creds.token)
	requireModelWithEndpoint(t, modelIndex, "gpt-5-mini", runtimeconfig.UpstreamChatCompletionsPath)
	engine := newLiveEngine(creds.token, modelResolver(modelIndex))

	body := []byte(`{"model":"gpt-5-mini","messages":[{"role":"user","content":"ping"}],"max_tokens":1,"stream":false}`)
	status, responseBody := executeLiveRequest(t, engine, runtimeconfig.ChatCompletionsPath, body)
	assertLiveSuccessStatusAndBody(t, status, responseBody)
}

func TestLiveSession_Responses_GPT5Mini(t *testing.T) {
	creds := requireLiveCredentials(t)
	modelIndex := fetchLiveModelIndex(t, creds.token)
	requireModelWithEndpoint(t, modelIndex, "gpt-5-mini", runtimeconfig.UpstreamResponsesPath)
	engine := newLiveEngine(creds.token, modelResolver(modelIndex))

	body := []byte(`{"model":"gpt-5-mini","input":"ping","max_output_tokens":1}`)
	status, responseBody := executeLiveRequest(t, engine, runtimeconfig.ResponsesPath, body)
	assertLiveSuccessStatusAndBody(t, status, responseBody)
}

func requireLiveEnabled(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(liveFlagEnv)) != "1" {
		t.Skipf("set %s=1 to run live integration tests", liveFlagEnv)
	}
}

func requireLiveCredentials(t *testing.T) liveCredentials {
	t.Helper()
	requireLiveEnabled(t)

	token := strings.TrimSpace(os.Getenv(liveTokenEnv))
	if token != "" {
		return liveCredentials{token: token}
	}

	authConfig, err := runtimeconfig.LoadAuth()
	if err != nil {
		t.Skipf("load auth config: %v", err)
	}
	account, _, err := authConfig.DefaultAccount()
	if err != nil {
		t.Skipf("resolve default account: %v", err)
	}
	token = strings.TrimSpace(account.GhToken)
	if token == "" {
		t.Skip("default account token is empty")
	}
	return liveCredentials{token: token}
}

func newLiveEngine(token string, resolveModel ResolveModelFunc) *Engine {
	return NewEngine(Options{
		SettingsProvider: func(context.Context) (runtimeconfig.RuntimeSettings, error) {
			return runtimeconfig.Default(), nil
		},
		HTTPClientFactory: func() *http.Client {
			return &http.Client{Timeout: 90 * time.Second}
		},
		ResolveToken: func(context.Context, string) (string, error) {
			return token, nil
		},
		ResolveModel: resolveModel,
	})
}

func fetchLiveModelIndex(t *testing.T, token string) map[string]models.ModelInfo {
	t.Helper()
	engine := newLiveEngine(token, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	items, err := engine.FetchModels(ctx, token)
	if err != nil {
		t.Fatalf("fetch models: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected non-empty model catalog")
	}
	index := make(map[string]models.ModelInfo, len(items))
	for i := range items {
		model := items[i]
		index[model.ID] = model
	}
	return index
}

func modelResolver(modelIndex map[string]models.ModelInfo) ResolveModelFunc {
	return func(_ context.Context, modelID string) (ModelInfo, error) {
		item, ok := modelIndex[modelID]
		if !ok {
			return ModelInfo{ID: modelID}, nil
		}
		return ModelInfo{
			ID:        item.ID,
			Endpoints: append([]string(nil), item.Endpoints...),
		}, nil
	}
}

func requireModelWithEndpoint(t *testing.T, modelIndex map[string]models.ModelInfo, modelID, endpoint string) {
	t.Helper()
	model, ok := modelIndex[modelID]
	if !ok {
		t.Skipf("model %q not found in live catalog", modelID)
	}
	if !containsEndpoint(model.Endpoints, endpoint) {
		t.Skipf("model %q does not support endpoint %q (supports %v)", modelID, endpoint, model.Endpoints)
	}
}

func containsEndpoint(endpoints []string, endpoint string) bool {
	for _, candidate := range endpoints {
		if candidate == endpoint {
			return true
		}
	}
	return false
}

func executeLiveRequest(t *testing.T, engine *Engine, path string, body []byte) (int, []byte) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var (
		callbackStatus int
		callbackBody   []byte
		callbackError  string
		callbacks      int
	)

	err := engine.Execute(ctx, RequestInvocation{
		Method: http.MethodPost,
		Path:   path,
		Header: map[string]string{"Content-Type": "application/json"},
		Body:   body,
	}, ExecuteOptions{
		Mode: StreamModeBuffered,
		ResultCallback: func(result ExecuteResult) {
			callbacks++
			callbackStatus = result.StatusCode
			callbackBody = append([]byte(nil), result.Body...)
			callbackError = result.Error
		},
	})
	if err != nil {
		t.Fatalf("execute %s: %v", path, err)
	}
	if callbacks != 1 {
		t.Fatalf("expected single callback for %s, got %d", path, callbacks)
	}
	if callbackError != "" {
		t.Fatalf("callback error for %s: %s", path, callbackError)
	}
	return callbackStatus, callbackBody
}

func assertLiveSuccessStatusAndBody(t *testing.T, status int, body []byte) {
	t.Helper()
	if status < 200 || status >= 300 {
		t.Fatalf("expected 2xx status, got %d with body %s", status, string(body))
	}
	if len(body) == 0 {
		t.Fatalf("expected non-empty response body")
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("expected JSON response body: %v; body=%s", err, string(body))
	}
}

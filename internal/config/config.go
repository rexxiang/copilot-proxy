package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"copilot-proxy/internal/reasoning"
)

type Settings struct {
	ListenAddr   string `json:"listen_addr" ui:"label=Listen;widget=text;visible=false;readonly=true;order=10"`
	UpstreamBase string `json:"upstream_base" ui:"label=Upstream;widget=url;visible=true;readonly=true;order=20"`
	// Map fields are storage-only in settings.json; TUI editing should use shadow array fields with ui tags.
	RequiredHeaders                   map[string]string    `json:"required_headers,omitempty" ui:"label=Headers;widget=kv;visible=false;readonly=false;order=60"`
	MaxRetries                        int                  `json:"max_retries" ui:"label=Retries;widget=int;visible=true;readonly=false;order=40;min=1;description=Max upstream retry attempts."`
	RetryBackoff                      Duration             `json:"retry_backoff" ui:"label=Backoff;widget=duration;visible=true;readonly=false;order=50;description=Initial retry delay."`
	RateLimitSeconds                  int                  `json:"rate_limit_seconds" ui:"label=Rate Limit (sec);widget=int;visible=true;readonly=false;order=52;min=0;placeholder=0;empty=zero;description=Cooldown seconds between completed requests. 0 or empty disables it."`
	MessagesAgentDetectionRequestMode bool                 `json:"messages_agent_detection_request_mode" ui:"label=Msg Agent Mode;widget=bool;visible=true;readonly=false;order=55"`
	ReasoningPoliciesMap              map[string]string    `json:"reasoning_policies,omitempty" ui:"label=ReasoningPoliciesMap;widget=kv;visible=false;readonly=false;order=65"`
	ReasoningPolicies                 []ReasoningPolicy    `json:"-" ui:"key=reasoning_policies_ui;label=Reasoning Policies;widget=array;visible=true;readonly=false;order=66;description=Per-model reasoning policies."`
	ClaudeHaikuFallbackModels         []string             `json:"-"`
	ClaudeHaikuFallbackModelsUI       []HaikuFallbackModel `json:"-" ui:"key=claude_haiku_fallback_models_ui;label=Haiku Fallbacks;widget=array;visible=true;readonly=false;order=67;description=Ordered replacements for claude-haiku-*."`
}

type ReasoningPolicy struct {
	Model  string `json:"model" ui:"label=Model;widget=text;visible=true;readonly=false;order=10;placeholder=gpt-5-mini"`
	Target string `json:"target" ui:"label=Target;widget=text;visible=true;readonly=false;order=20;enum=chat,responses"`
	Effort string `json:"effort" ui:"label=Effort;widget=text;visible=true;readonly=false;order=30;enum=none,low,medium,high"`
}

type HaikuFallbackModel struct {
	Model string `json:"model" ui:"label=Model;widget=text;visible=true;readonly=false;order=10;placeholder=gpt-5-mini"`
}

type Account struct {
	User    string `json:"user"`
	GhToken string `json:"gh_token"`
	AppID   string `json:"app_id"`
}

type AuthConfig struct {
	Default  string    `json:"@default"`
	Accounts []Account `json:"accounts"`
}

const (
	configFileMode = 0o600
	configDirMode  = 0o700
)

var defaultClaudeHaikuFallbackModels = []string{"gpt-5-mini", "grok-code-fast-1"}

var (
	ErrAccountNotFound        = errors.New("account not found")
	ErrDefaultAccountNotFound = errors.New("default account not found")
	ErrInvalidConfigPath      = errors.New("invalid config path")
	ErrNoAccountsConfigured   = errors.New("no accounts configured")
)

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".config", "copilot-proxy"), nil
}

func AuthPath() (string, error) {
	return configPath("auth.json")
}

func SettingsPath() (string, error) {
	return configPath("settings.json")
}

// loadJSON reads and unmarshals JSON from a config file.
func loadJSON[T any](pathFunc func() (string, error), defaultVal T) (T, error) {
	path, err := pathFunc()
	if err != nil {
		return defaultVal, fmt.Errorf("resolve config path: %w", err)
	}
	if !isConfigPath(path) {
		return defaultVal, fmt.Errorf("%w: %s", ErrInvalidConfigPath, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultVal, nil
		}
		return defaultVal, fmt.Errorf("read config file: %w", err)
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return defaultVal, fmt.Errorf("decode config: %w", err)
	}
	return result, nil
}

// saveJSON marshals and writes JSON to a config file.
func saveJSON[T any](pathFunc func() (string, error), value T) error {
	path, err := pathFunc()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := ensureConfigDir(); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func LoadSettings() (Settings, error) {
	settings, err := loadJSON(SettingsPath, DefaultSettings())
	if err != nil {
		return Settings{}, err
	}
	loaded := applyDefaults(&settings)
	if err := loaded.syncReasoningPoliciesFromMap(); err != nil {
		return Settings{}, fmt.Errorf("decode reasoning policies: %w", err)
	}
	loaded.syncClaudeHaikuFallbackModelsFromStorage()
	return loaded, nil
}

func SaveSettings(settings *Settings) error {
	if settings == nil {
		return ErrInvalidConfigPath
	}
	sanitized := applyDefaults(settings)
	if err := sanitized.syncReasoningPoliciesToMap(); err != nil {
		return fmt.Errorf("encode reasoning policies: %w", err)
	}
	sanitized.syncClaudeHaikuFallbackModelsToStorage()
	return saveJSON(SettingsPath, sanitized)
}

func DefaultSettings() Settings {
	settings := Settings{
		ListenAddr:                        DefaultListenAddr,
		UpstreamBase:                      CopilotAPIURL,
		RequiredHeaders:                   nil,
		MaxRetries:                        DefaultMaxRetries,
		RetryBackoff:                      NewDuration(DefaultRetryBackoff),
		RateLimitSeconds:                  0,
		MessagesAgentDetectionRequestMode: true,
		ReasoningPoliciesMap:              nil,
		ReasoningPolicies:                 nil,
		ClaudeHaikuFallbackModels:         cloneStringSlice(defaultClaudeHaikuFallbackModels),
		ClaudeHaikuFallbackModelsUI:       nil,
	}
	settings.syncClaudeHaikuFallbackModelsFromStorage()
	return settings
}

func LoadAuth() (AuthConfig, error) {
	return loadJSON(AuthPath, AuthConfig{})
}

func SaveAuth(auth AuthConfig) error {
	return saveJSON(AuthPath, auth)
}

func (auth *AuthConfig) EnsureDefault() bool {
	if len(auth.Accounts) == 0 {
		return false
	}

	for _, account := range auth.Accounts {
		if account.User == auth.Default {
			return false
		}
	}

	auth.Default = auth.Accounts[0].User
	return true
}

func (auth *AuthConfig) DefaultAccount() (Account, bool, error) {
	if len(auth.Accounts) == 0 {
		return Account{}, false, ErrNoAccountsConfigured
	}

	changed := auth.EnsureDefault()
	for _, account := range auth.Accounts {
		if account.User == auth.Default {
			return account, changed, nil
		}
	}

	return Account{}, changed, ErrDefaultAccountNotFound
}

func applyDefaults(settings *Settings) Settings {
	if settings == nil {
		settings = &Settings{}
	}
	s := *settings
	if s.ListenAddr == "" {
		s.ListenAddr = DefaultListenAddr
	}
	if s.UpstreamBase == "" {
		s.UpstreamBase = CopilotAPIURL
	}
	if s.MaxRetries <= 0 {
		s.MaxRetries = DefaultMaxRetries
	}
	if !s.RetryBackoff.IsSet() {
		s.RetryBackoff = NewDuration(DefaultRetryBackoff)
	}
	if s.RateLimitSeconds < 0 {
		s.RateLimitSeconds = 0
	}
	if s.ClaudeHaikuFallbackModels == nil {
		s.ClaudeHaikuFallbackModels = cloneStringSlice(defaultClaudeHaikuFallbackModels)
	}
	return s
}

func (settings Settings) MarshalJSON() ([]byte, error) {
	type settingsJSON struct {
		ListenAddr                        string            `json:"listen_addr"`
		UpstreamBase                      string            `json:"upstream_base"`
		RequiredHeaders                   map[string]string `json:"required_headers,omitempty"`
		MaxRetries                        int               `json:"max_retries"`
		RetryBackoff                      Duration          `json:"retry_backoff"`
		RateLimitSeconds                  int               `json:"rate_limit_seconds"`
		MessagesAgentDetectionRequestMode *bool             `json:"messages_agent_detection_request_mode"`
		ReasoningPoliciesMap              map[string]string `json:"reasoning_policies,omitempty"`
		ClaudeHaikuFallbackModels         []string          `json:"claude_haiku_fallback_models"`
	}

	messagesAgentDetectionRequestMode := settings.MessagesAgentDetectionRequestMode
	payload := settingsJSON{
		ListenAddr:                        settings.ListenAddr,
		UpstreamBase:                      settings.UpstreamBase,
		RequiredHeaders:                   settings.RequiredHeaders,
		MaxRetries:                        settings.MaxRetries,
		RetryBackoff:                      settings.RetryBackoff,
		RateLimitSeconds:                  settings.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: &messagesAgentDetectionRequestMode,
		ReasoningPoliciesMap:              settings.ReasoningPoliciesMap,
		ClaudeHaikuFallbackModels:         settings.ClaudeHaikuFallbackModels,
	}
	return json.Marshal(payload)
}

func (settings *Settings) UnmarshalJSON(data []byte) error {
	type settingsJSON struct {
		ListenAddr                        string            `json:"listen_addr"`
		UpstreamBase                      string            `json:"upstream_base"`
		RequiredHeaders                   map[string]string `json:"required_headers,omitempty"`
		MaxRetries                        int               `json:"max_retries"`
		RetryBackoff                      Duration          `json:"retry_backoff"`
		RateLimitSeconds                  int               `json:"rate_limit_seconds"`
		MessagesAgentDetectionRequestMode *bool             `json:"messages_agent_detection_request_mode"`
		ReasoningPoliciesMap              map[string]string `json:"reasoning_policies,omitempty"`
		ClaudeHaikuFallbackModels         []string          `json:"claude_haiku_fallback_models"`
	}

	var payload settingsJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	*settings = Settings{
		ListenAddr:                        payload.ListenAddr,
		UpstreamBase:                      payload.UpstreamBase,
		RequiredHeaders:                   payload.RequiredHeaders,
		MaxRetries:                        payload.MaxRetries,
		RetryBackoff:                      payload.RetryBackoff,
		RateLimitSeconds:                  payload.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: true,
		ReasoningPoliciesMap:              payload.ReasoningPoliciesMap,
		ClaudeHaikuFallbackModels:         payload.ClaudeHaikuFallbackModels,
	}
	if payload.MessagesAgentDetectionRequestMode != nil {
		settings.MessagesAgentDetectionRequestMode = *payload.MessagesAgentDetectionRequestMode
	}
	return nil
}

func configPath(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func ensureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return nil
}

func isConfigPath(path string) bool {
	if path == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	configDir, err := ConfigDir()
	if err != nil {
		return false
	}
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absConfigDir, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, "..")
}

func (auth *AuthConfig) UpsertAccount(account Account) {
	for i, existing := range auth.Accounts {
		if existing.User == account.User {
			auth.Accounts[i] = account
			auth.Default = account.User
			return
		}
	}
	auth.Accounts = append(auth.Accounts, account)
	auth.Default = account.User
}

func (auth *AuthConfig) RemoveAccount(user string) bool {
	if user == "" {
		return false
	}
	for i, account := range auth.Accounts {
		if account.User == user {
			auth.Accounts = append(auth.Accounts[:i], auth.Accounts[i+1:]...)
			if auth.Default == user {
				auth.Default = ""
				auth.EnsureDefault()
			}
			return true
		}
	}
	return false
}

func (auth *AuthConfig) SetDefault(user string) error {
	for _, account := range auth.Accounts {
		if account.User == user {
			auth.Default = user
			return nil
		}
	}
	return ErrAccountNotFound
}

func DefaultProxyHeaders() map[string]string {
	return map[string]string{
		"user-agent":             DefaultUserAgent,
		"copilot-integration-id": DefaultIntegrationID,
	}
}

func (settings *Settings) RequiredHeadersWithDefaults() map[string]string {
	defaults := DefaultProxyHeaders()
	if settings == nil {
		return defaults
	}
	for key, value := range normalizeHeaders(settings.RequiredHeaders) {
		defaults[key] = value
	}
	return defaults
}

func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		normalized[strings.ToLower(key)] = value
	}
	return normalized
}

func (settings *Settings) syncReasoningPoliciesFromMap() error {
	if settings == nil {
		return nil
	}
	parsed, err := reasoning.ParsePolicyMap(settings.ReasoningPoliciesMap)
	if err != nil {
		return err
	}
	if len(parsed) == 0 {
		settings.ReasoningPolicies = nil
		return nil
	}
	settings.ReasoningPolicies = make([]ReasoningPolicy, 0, len(parsed))
	for _, item := range parsed {
		settings.ReasoningPolicies = append(settings.ReasoningPolicies, ReasoningPolicy{
			Model:  item.Model,
			Target: item.Target,
			Effort: item.Effort,
		})
	}
	return nil
}

func (settings *Settings) syncReasoningPoliciesToMap() error {
	if settings == nil {
		return nil
	}
	items := make([]reasoning.Policy, 0, len(settings.ReasoningPolicies))
	for _, item := range settings.ReasoningPolicies {
		items = append(items, reasoning.Policy{
			Model:  item.Model,
			Target: item.Target,
			Effort: item.Effort,
		})
	}
	encoded, err := reasoning.BuildPolicyMap(items)
	if err != nil {
		return err
	}
	settings.ReasoningPoliciesMap = encoded
	return nil
}

func (settings *Settings) syncClaudeHaikuFallbackModelsFromStorage() {
	if settings == nil {
		return
	}
	normalized := normalizeStringSlice(settings.ClaudeHaikuFallbackModels)
	if len(normalized) == 0 {
		settings.ClaudeHaikuFallbackModelsUI = nil
		return
	}
	rows := make([]HaikuFallbackModel, 0, len(normalized))
	for _, model := range normalized {
		rows = append(rows, HaikuFallbackModel{Model: model})
	}
	settings.ClaudeHaikuFallbackModelsUI = rows
}

func (settings *Settings) syncClaudeHaikuFallbackModelsToStorage() {
	if settings == nil || settings.ClaudeHaikuFallbackModelsUI == nil {
		return
	}
	models := make([]string, 0, len(settings.ClaudeHaikuFallbackModelsUI))
	for _, item := range settings.ClaudeHaikuFallbackModelsUI {
		model := strings.TrimSpace(item.Model)
		if model == "" {
			continue
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		settings.ClaudeHaikuFallbackModels = []string{}
		return
	}
	settings.ClaudeHaikuFallbackModels = models
}

func cloneStringSlice(items []string) []string {
	if items == nil {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

func normalizeStringSlice(items []string) []string {
	if items == nil {
		return nil
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return []string{}
	}
	return normalized
}

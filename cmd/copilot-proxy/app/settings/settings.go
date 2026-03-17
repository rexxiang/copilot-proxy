package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"copilot-proxy/internal/core/runtimeconfig"
	"copilot-proxy/internal/reasoning"
)

const (
	configFileMode = 0o600
	configDirMode  = 0o700
)

var (
	ErrInvalidSettingsPath = errors.New("invalid settings path")
)

type Settings struct {
	ListenAddr                        string               `json:"listen_addr"`
	UpstreamBase                      string               `json:"upstream_base"`
	RequiredHeaders                   map[string]string    `json:"required_headers,omitempty"`
	MaxRetries                        int                  `json:"max_retries"`
	RetryBackoff                      Duration             `json:"retry_backoff"`
	RateLimitSeconds                  int                  `json:"rate_limit_seconds"`
	MessagesAgentDetectionRequestMode bool                 `json:"messages_agent_detection_request_mode"`
	ReasoningPoliciesMap              map[string]string    `json:"reasoning_policies,omitempty"`
	ReasoningPolicies                 []ReasoningPolicy    `json:"-"`
	ClaudeHaikuFallbackModels         []string             `json:"-"`
	ClaudeHaikuFallbackModelsUI       []HaikuFallbackModel `json:"-"`
}

type ReasoningPolicy struct {
	Model  string `json:"model"`
	Target string `json:"target"`
	Effort string `json:"effort"`
}

type HaikuFallbackModel struct {
	Model string `json:"model"`
}

type Duration = runtimeconfig.Duration

var NewDuration = runtimeconfig.NewDuration

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".config", "copilot-proxy"), nil
}

func SettingsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func LoadSettings() (Settings, error) {
	current, err := loadJSON(SettingsPath, DefaultSettings())
	if err != nil {
		return Settings{}, err
	}
	loaded := applyDefaults(&current)
	if err := loaded.syncReasoningPoliciesFromMap(); err != nil {
		return Settings{}, fmt.Errorf("decode reasoning policies: %w", err)
	}
	loaded.SyncViewFieldsFromStorage()
	return loaded, nil
}

func SaveSettings(settings *Settings) error {
	if settings == nil {
		return ErrInvalidSettingsPath
	}
	sanitized := applyDefaults(settings)
	if err := sanitized.SyncStorageFieldsFromView(); err != nil {
		return fmt.Errorf("encode settings shadows: %w", err)
	}
	return saveJSON(SettingsPath, sanitized)
}

func DefaultSettings() Settings {
	defaults := runtimeconfig.Default()
	return FromRuntimeConfig(defaults)
}

func ToRuntimeConfig(settings Settings) runtimeconfig.Config {
	clone := applyDefaults(&settings)
	_ = clone.SyncStorageFieldsFromView()
	return runtimeconfig.Config{
		ListenAddr:                        clone.ListenAddr,
		UpstreamBase:                      clone.UpstreamBase,
		RequiredHeaders:                   cloneStringMap(clone.RequiredHeaders),
		MaxRetries:                        clone.MaxRetries,
		RetryBackoff:                      clone.RetryBackoff,
		RateLimitSeconds:                  clone.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: clone.MessagesAgentDetectionRequestMode,
		ReasoningPoliciesMap:              cloneStringMap(clone.ReasoningPoliciesMap),
		ClaudeHaikuFallbackModels:         cloneStringSlice(clone.ClaudeHaikuFallbackModels),
	}
}

func FromRuntimeConfig(cfg runtimeconfig.Config) Settings {
	defaults := runtimeconfig.Default()
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaults.ListenAddr
	}
	if cfg.UpstreamBase == "" {
		cfg.UpstreamBase = defaults.UpstreamBase
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if !cfg.RetryBackoff.IsSet() {
		cfg.RetryBackoff = defaults.RetryBackoff
	}
	if cfg.RateLimitSeconds < 0 {
		cfg.RateLimitSeconds = 0
	}
	if cfg.ClaudeHaikuFallbackModels == nil {
		cfg.ClaudeHaikuFallbackModels = cloneStringSlice(defaults.ClaudeHaikuFallbackModels)
	}
	settings := Settings{
		ListenAddr:                        cfg.ListenAddr,
		UpstreamBase:                      cfg.UpstreamBase,
		RequiredHeaders:                   cloneStringMap(cfg.RequiredHeaders),
		MaxRetries:                        cfg.MaxRetries,
		RetryBackoff:                      cfg.RetryBackoff,
		RateLimitSeconds:                  cfg.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: cfg.MessagesAgentDetectionRequestMode,
		ReasoningPoliciesMap:              cloneStringMap(cfg.ReasoningPoliciesMap),
		ClaudeHaikuFallbackModels:         cloneStringSlice(cfg.ClaudeHaikuFallbackModels),
	}
	if err := settings.syncReasoningPoliciesFromMap(); err != nil {
		settings.ReasoningPolicies = nil
	}
	settings.SyncViewFieldsFromStorage()
	return settings
}

func (settings *Settings) SyncViewFieldsFromStorage() {
	if settings == nil {
		return
	}
	settings.syncClaudeHaikuFallbackModelsFromStorage()
}

func (settings *Settings) SyncStorageFieldsFromView() error {
	if settings == nil {
		return nil
	}
	if err := settings.syncReasoningPoliciesToMap(); err != nil {
		return err
	}
	settings.syncClaudeHaikuFallbackModelsToStorage()
	return nil
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

	current := applyDefaults(&settings)
	if err := current.SyncStorageFieldsFromView(); err != nil {
		return nil, fmt.Errorf("sync storage fields: %w", err)
	}
	requestMode := current.MessagesAgentDetectionRequestMode
	return json.Marshal(settingsJSON{
		ListenAddr:                        current.ListenAddr,
		UpstreamBase:                      current.UpstreamBase,
		RequiredHeaders:                   current.RequiredHeaders,
		MaxRetries:                        current.MaxRetries,
		RetryBackoff:                      current.RetryBackoff,
		RateLimitSeconds:                  current.RateLimitSeconds,
		MessagesAgentDetectionRequestMode: &requestMode,
		ReasoningPoliciesMap:              current.ReasoningPoliciesMap,
		ClaudeHaikuFallbackModels:         current.ClaudeHaikuFallbackModels,
	})
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

func loadJSON[T any](pathFunc func() (string, error), defaultVal T) (T, error) {
	path, err := pathFunc()
	if err != nil {
		return defaultVal, fmt.Errorf("resolve config path: %w", err)
	}
	if !isSettingsPath(path) {
		return defaultVal, fmt.Errorf("%w: %s", ErrInvalidSettingsPath, path)
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

func applyDefaults(settings *Settings) Settings {
	if settings == nil {
		settings = &Settings{}
	}
	defaults := runtimeconfig.Default()
	current := *settings
	if current.ListenAddr == "" {
		current.ListenAddr = defaults.ListenAddr
	}
	if current.UpstreamBase == "" {
		current.UpstreamBase = defaults.UpstreamBase
	}
	if current.MaxRetries <= 0 {
		current.MaxRetries = defaults.MaxRetries
	}
	if !current.RetryBackoff.IsSet() {
		current.RetryBackoff = defaults.RetryBackoff
	}
	if current.RateLimitSeconds < 0 {
		current.RateLimitSeconds = 0
	}
	if current.ClaudeHaikuFallbackModels == nil {
		current.ClaudeHaikuFallbackModels = cloneStringSlice(defaults.ClaudeHaikuFallbackModels)
	}
	return current
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

func isSettingsPath(path string) bool {
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

func cloneStringMap(items map[string]string) map[string]string {
	if items == nil {
		return nil
	}
	cloned := make(map[string]string, len(items))
	for key, value := range items {
		cloned[key] = value
	}
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

func DefaultProxyHeaders() map[string]string {
	return runtimeconfig.DefaultProxyHeaders()
}

func (settings *Settings) RequiredHeadersWithDefaults() map[string]string {
	cfg := ToRuntimeConfig(applyDefaults(settings))
	return cfg.RequiredHeadersWithDefaults()
}

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
	RequiredHeaders      map[string]string `json:"required_headers,omitempty" ui:"label=Headers;widget=kv;visible=false;readonly=false;order=60"`
	UpstreamTimeout      Duration          `json:"upstream_timeout" ui:"label=Timeout;widget=duration;visible=true;readonly=false;order=30"`
	MaxRetries           int               `json:"max_retries" ui:"label=Retries;widget=int;visible=true;readonly=false;order=40;min=1"`
	RetryBackoff         Duration          `json:"retry_backoff" ui:"label=Backoff;widget=duration;visible=true;readonly=false;order=50"`
	MessagesInitSeqAgent bool              `json:"messages_init_seq_agent" ui:"label=MsgInitSeqAgent;widget=bool;visible=true;readonly=false;order=55;placeholder=true|false"`
	ReasoningPoliciesMap map[string]string `json:"reasoning_policies,omitempty" ui:"label=ReasoningPoliciesMap;widget=kv;visible=false;readonly=false;order=65"`
	ReasoningPolicies    []ReasoningPolicy `json:"-" ui:"key=reasoning_policies_ui;label=ReasoningPolicies;widget=array;visible=true;readonly=false;order=66;description=UI shadow list for reasoning_policies map. Use model@target rules with effort none|low|medium|high."`
}

type ReasoningPolicy struct {
	Model  string `json:"model" ui:"label=Model;widget=text;visible=true;readonly=false;order=10;placeholder=gpt-5-mini"`
	Target string `json:"target" ui:"label=Target;widget=text;visible=true;readonly=false;order=20;enum=chat,responses"`
	Effort string `json:"effort" ui:"label=Effort;widget=text;visible=true;readonly=false;order=30;enum=none,low,medium,high"`
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
	settings, err := loadJSON(SettingsPath, Settings{})
	if err != nil {
		return Settings{}, err
	}
	loaded := applyDefaults(&settings)
	if err := loaded.syncReasoningPoliciesFromMap(); err != nil {
		return Settings{}, fmt.Errorf("decode reasoning policies: %w", err)
	}
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
	return saveJSON(SettingsPath, sanitized)
}

func DefaultSettings() Settings {
	return Settings{
		ListenAddr:           DefaultListenAddr,
		UpstreamBase:         CopilotAPIURL,
		RequiredHeaders:      nil,
		UpstreamTimeout:      NewDuration(DefaultUpstreamTimeout),
		MaxRetries:           DefaultMaxRetries,
		RetryBackoff:         NewDuration(DefaultRetryBackoff),
		MessagesInitSeqAgent: false,
		ReasoningPoliciesMap: nil,
		ReasoningPolicies:    nil,
	}
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
	if !s.UpstreamTimeout.IsSet() {
		s.UpstreamTimeout = NewDuration(DefaultUpstreamTimeout)
	}
	if s.MaxRetries <= 0 {
		s.MaxRetries = DefaultMaxRetries
	}
	if !s.RetryBackoff.IsSet() {
		s.RetryBackoff = NewDuration(DefaultRetryBackoff)
	}
	return s
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

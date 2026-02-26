package config

import "time"

// GitHub OAuth.
const (
	GitHubBaseURL = "https://github.com"
	GitHubAPIURL  = "https://api.github.com"
	CopilotAPIURL = "https://api.githubcopilot.com"
	OAuthClientID = "Iv1.b507a08c87ecfe98"
	OAuthScope    = "read:user"
)

// API Paths - Local (OpenAI-compatible) endpoints.
const (
	ChatCompletionsPath = "/v1/chat/completions"
	ResponsesPath       = "/v1/responses"
	MessagesPath        = "/v1/messages"
	ModelsPath          = "/copilot/models"
)

// Upstream Copilot API paths (no /v1 prefix).
const (
	UpstreamChatCompletionsPath = "/chat/completions"
	UpstreamResponsesPath       = "/responses"
	UpstreamMessagesPath        = "/v1/messages"
	UpstreamModelsPath          = "/models"
)

// Timeouts and Durations.
const (
	DefaultUpstreamTimeout     = 5 * time.Minute
	ShutdownTimeout            = 5 * time.Second
	DefaultRetryBackoff        = 1 * time.Second
	DefaultMaxRetries          = 3
	DefaultRetryInitialBackoff = 100 * time.Millisecond
	DefaultRetryMaxBackoff     = 5 * time.Second
	DefaultRetryBackoffFactor  = 2.0
)

// Default Headers.
const (
	DefaultUserAgent     = "copilot/0.0.400"
	DefaultIntegrationID = "copilot-developer-cli"
)

// Server Defaults.
const (
	DefaultListenAddr = "127.0.0.1:4000"
)

// AllowedPaths contains the API paths that are allowed to be proxied.
var AllowedPaths = []string{ChatCompletionsPath, ResponsesPath, MessagesPath, ModelsPath}

// PathMapping maps local paths to upstream Copilot API paths.
var PathMapping = map[string]string{
	ChatCompletionsPath: UpstreamChatCompletionsPath,
	ResponsesPath:       UpstreamResponsesPath,
	MessagesPath:        UpstreamMessagesPath,
	ModelsPath:          UpstreamModelsPath,
}

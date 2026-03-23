package config

import (
	"time"

	protocolpaths "copilot-proxy/internal/runtime/protocol/paths"
)

// GitHub OAuth.
const (
	GitHubBaseURL = "https://github.com"
	GitHubAPIURL  = "https://api.github.com"
	CopilotAPIURL = "https://api.githubcopilot.com"
	OAuthClientID = "Iv1.b507a08c87ecfe98"
	OAuthScope    = "read:user"
)

// API Paths - Local (OpenAI-compatible) endpoints.
// Deprecated: use internal/runtime/protocol/paths.
const (
	ChatCompletionsPath = protocolpaths.ChatCompletionsPath
	ResponsesPath       = protocolpaths.ResponsesPath
	MessagesPath        = protocolpaths.MessagesPath
	ModelsPath          = protocolpaths.ModelsPath
)

// Upstream Copilot API paths.
// Deprecated: use internal/runtime/protocol/paths.
const (
	UpstreamChatCompletionsPath = protocolpaths.UpstreamChatCompletionsPath
	UpstreamResponsesPath       = protocolpaths.UpstreamResponsesPath
	UpstreamMessagesPath        = protocolpaths.UpstreamMessagesPath
	UpstreamModelsPath          = protocolpaths.UpstreamModelsPath
)

// Timeouts and Durations.
const (
	ShutdownTimeout            = 5 * time.Second
	DefaultRetryBackoff        = 1 * time.Second
	DefaultMaxRetries          = 3
	DefaultRetryInitialBackoff = 100 * time.Millisecond
	DefaultRetryMaxBackoff     = 5 * time.Second
	DefaultRetryBackoffFactor  = 2.0
)

// Default Headers.
const (
	DefaultUserAgent     = "copilot/1.0.2"
	DefaultIntegrationID = "copilot-developer-cli"
)

// Server Defaults.
const (
	DefaultListenAddr = "127.0.0.1:4000"
)

// AllowedPaths contains the API paths that are allowed to be proxied.
// Deprecated: use internal/runtime/protocol/paths.AllowedLocalPaths.
var AllowedPaths = protocolpaths.AllowedLocalPaths()

// PathMapping maps local paths to upstream Copilot API paths.
// Deprecated: use internal/runtime/protocol/paths.DefaultPathMapping.
var PathMapping = protocolpaths.DefaultPathMapping()

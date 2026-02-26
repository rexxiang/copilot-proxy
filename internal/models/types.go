package models

// ModelInfo describes an available Copilot model.
type ModelInfo struct {
	ID              string   // gpt-4o, claude-3-opus, etc.
	Name            string   // Display name
	Vendor          string   // OpenAI, Anthropic, Google, etc.
	Endpoints       []string // Supported endpoints: /chat/completions, /responses, /v1/messages
	IsPremium       bool     // Requires premium subscription
	Multiplier      float64  // Premium quota multiplier
	Preview         bool     // Is preview model
	Family          string   // Model family (gpt-4o, claude-sonnet-4.5, etc.)
	ContextWindow   int      // Max context window tokens (e.g., 128000, 400000)
	MaxPromptTokens int      // Max input context window tokens
	MaxOutputTokens int      // Max output context window tokens
}

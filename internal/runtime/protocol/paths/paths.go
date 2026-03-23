package paths

// Local API paths (client-facing OpenAI/Anthropic compatible endpoints).
const (
	ChatCompletionsPath = "/v1/chat/completions"
	ResponsesPath       = "/v1/responses"
	MessagesPath        = "/v1/messages"
	ModelsPath          = "/copilot/models"
)

// Upstream Copilot API paths.
const (
	UpstreamChatCompletionsPath = "/chat/completions"
	UpstreamResponsesPath       = "/responses"
	UpstreamMessagesPath        = "/v1/messages"
	UpstreamModelsPath          = "/models"
)

var allowedLocalPaths = []string{
	ChatCompletionsPath,
	ResponsesPath,
	MessagesPath,
	ModelsPath,
}

var defaultPathMapping = map[string]string{
	ChatCompletionsPath: UpstreamChatCompletionsPath,
	ResponsesPath:       UpstreamResponsesPath,
	MessagesPath:        UpstreamMessagesPath,
	ModelsPath:          UpstreamModelsPath,
}

// AllowedLocalPaths returns the local API paths accepted by the server.
func AllowedLocalPaths() []string {
	return cloneStringSlice(allowedLocalPaths)
}

// DefaultPathMapping returns local -> upstream path mapping.
func DefaultPathMapping() map[string]string {
	return cloneStringMap(defaultPathMapping)
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	out := make([]string, len(input))
	copy(out, input)
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

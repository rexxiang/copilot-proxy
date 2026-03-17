package transform

import "copilot-proxy/internal/reasoning"

// MessagesReasoningOptions controls reasoning-effort conversion for messages payloads.
type MessagesReasoningOptions struct {
	PolicyEffort             string
	SupportedReasoningEffort []string
}

func resolveMessagesReasoningEffort(outputConfig map[string]any, options MessagesReasoningOptions) (string, bool) {
	clientEffort, hasClientEffort := extractOutputConfigEffort(outputConfig)

	candidate := reasoning.EffortNone
	if hasClientEffort {
		candidate = reasoning.NormalizeClientEffort(clientEffort)
	} else if policyEffort, ok := reasoning.NormalizePolicyEffort(options.PolicyEffort); ok {
		candidate = policyEffort
	}

	return reasoning.ResolveMappedEffort(candidate, options.SupportedReasoningEffort)
}

func extractOutputConfigEffort(outputConfig map[string]any) (string, bool) {
	if outputConfig == nil {
		return "", false
	}
	raw, exists := outputConfig["effort"]
	if !exists {
		return "", false
	}
	effort, _ := raw.(string)
	return effort, true
}

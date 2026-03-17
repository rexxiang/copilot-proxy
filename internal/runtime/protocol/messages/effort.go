package messages

import "strings"

const (
	normalizedEffortLow    = "low"
	normalizedEffortMedium = "medium"
	normalizedEffortHigh   = "high"
)

// NormalizeEffort normalizes reasoning effort values across endpoint formats.
func NormalizeEffort(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "minimal":
		return normalizedEffortLow, true
	case "low":
		return normalizedEffortLow, true
	case "medium":
		return normalizedEffortMedium, true
	case "high":
		return normalizedEffortHigh, true
	case "max":
		return normalizedEffortHigh, true
	default:
		return "", false
	}
}

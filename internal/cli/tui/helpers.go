package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	truncateEllipsisLen = 3
	endpointAbbrevMax   = 10
	tokensPerMillion    = 1_000_000
	tokensPerThousand   = 1_000
	progressBarFilled   = "■"
	progressBarEmpty    = "□"
)

// Helper functions moved from monitor.go

func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= truncateEllipsisLen {
		return s[:maxLen]
	}
	return s[:maxLen-truncateEllipsisLen] + "..."
}

func FormatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// FormatEndpoints converts endpoint paths to abbreviated format.
// C=/chat/completions, R=/responses, M=/v1/messages.
func FormatEndpoints(endpoints []string) string {
	var abbrevs []string
	for _, ep := range endpoints {
		switch ep {
		case "/chat/completions":
			abbrevs = append(abbrevs, "C")
		case "/responses":
			abbrevs = append(abbrevs, "R")
		case "/v1/messages":
			abbrevs = append(abbrevs, "M")
		default:
			// Keep unknown endpoints abbreviated
			if len(ep) > endpointAbbrevMax {
				abbrevs = append(abbrevs, ep[:endpointAbbrevMax])
			} else {
				abbrevs = append(abbrevs, ep)
			}
		}
	}
	sort.Strings(abbrevs)
	return strings.Join(abbrevs, " ")
}

// FormatContextWindow converts token count to human-readable format (e.g., 128K, 400K).
func FormatContextWindow(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens >= tokensPerMillion {
		return fmt.Sprintf("%.1fM", float64(tokens)/tokensPerMillion)
	}
	return fmt.Sprintf("%dK", tokens/tokensPerThousand)
}

// FormatPromptOutputContext formats input/output token limits as "↑ 128K ↓ 16K".
func FormatPromptOutputContext(promptTokens, outputTokens int) string {
	if promptTokens <= 0 && outputTokens <= 0 {
		return "-"
	}
	prompt := FormatContextWindow(promptTokens)
	output := FormatContextWindow(outputTokens)
	return fmt.Sprintf("↑ %s ↓ %s", prompt, output)
}

// FormatContextSummary combines total context with prompt/output limits.
// Example: "128K, ↑ 128K ↓ 16K".
func FormatContextSummary(contextTokens, promptTokens, outputTokens int) string {
	ctx := FormatContextWindow(contextTokens)
	po := FormatPromptOutputContext(promptTokens, outputTokens)
	if po == "-" {
		return ctx
	}
	return fmt.Sprintf("%s, %s", ctx, po)
}

func RenderProgressBar(percent float64, width int) string {
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	bar := ProgressBarStyle.Render(strings.Repeat(progressBarFilled, filled)) +
		DimStyle.Render(strings.Repeat(progressBarEmpty, empty))
	return bar
}

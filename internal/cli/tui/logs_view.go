package tui

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"copilot-proxy/internal/monitor"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type LogsView struct {
	width        int
	height       int
	state        *SharedState
	offset       int
	debugLogger  *monitor.DebugLogger
	debugEnabled bool
}

const (
	logsHeaderSeparatorWidth = 94
	logsTimestampWidth       = 9
	logsModelWidth           = 24
	logsRequestWidth         = 24
	logsStatusErrorMin       = 400
	logsDurationWidth        = 8
	logsDefaultVisible       = 10
	logsFooterOffset         = 8
	logsScrollHint           = "\u2191\u2193 PgUp/PgDn Home/End g/G"
)

var logsPlainStyle = lipgloss.NewStyle()

func NewLogsView() *LogsView {
	return &LogsView{}
}

//goland:noinspection GoUnusedParameter
func (v *LogsView) Update(msg tea.Msg) (ViewComponent, tea.Cmd) {
	return v, nil
}

func (v *LogsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n")

	// Table header (same style as Models view)
	sb.WriteString(TableHeaderStyle.Render(fmt.Sprintf(
		"%-9s %-24s %-24s %-5s %4s %8s %8s",
		"Timestamp",
		"Model",
		"Request",
		"Type",
		"Code",
		"Duration",
		"Stream",
	)))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", logsHeaderSeparatorWidth))
	sb.WriteString("\n")

	// Merge active and completed requests
	allRecords := v.mergeActiveAndCompleted()

	showing := 0
	if len(allRecords) == 0 {
		sb.WriteString(DimStyle.Render("  No requests logged yet\n"))
	} else {
		// Build premium lookup for model-noise rendering.
		premiumByModel := make(map[string]bool, len(v.state.Models))
		for i := range v.state.Models {
			model := &v.state.Models[i]
			premiumByModel[model.ID] = model.IsPremium
		}

		// Calculate visible range
		visible := v.VisibleLines()
		if visible < 1 {
			visible = logsDefaultVisible
		}

		start := v.offset
		end := start + visible
		if end > len(allRecords) {
			end = len(allRecords)
		}
		showing = end - start

		for i := start; i < end; i++ {
			record := &allRecords[i]
			strikeRow := shouldStrikeRequestRow(record.StatusCode)
			timestamp := record.Timestamp.Format("15:04:05")
			model := Truncate(record.Model, logsModelWidth)
			if model == "" {
				model = "-"
			}
			timeStyle, modelStyle := stylePrimaryColumns(record, premiumByModel)
			timeCol := renderCell(fmt.Sprintf("%-*s", logsTimestampWidth, timestamp), timeStyle, strikeRow)
			modelCol := renderCell(fmt.Sprintf("%-*s", logsModelWidth, model), modelStyle, strikeRow)

			// Request method + upstream path - format width first, then apply style
			requestPath := record.UpstreamPath
			if requestPath == "" {
				requestPath = record.Path
			}
			if requestPath == "" {
				requestPath = "-"
			}
			method := record.Method
			if method == "" {
				method = "POST" // Default for legacy records
			}
			requestText := Truncate(method+" "+requestPath, logsRequestWidth)
			requestInfo := renderCell(fmt.Sprintf("%-*s", logsRequestWidth, requestText), &DimStyle, strikeRow)

			initiatorText, initiatorStyle := renderInitiator(model, record.IsAgent)
			initiator := renderCell(initiatorText, initiatorStyle, strikeRow)

			statusText, statusStyle := renderStatus(record.StatusCode)
			status := renderCell(statusText, statusStyle, strikeRow)

			durationText, durationStyle := renderDuration(record)
			duration := renderCell(durationText, durationStyle, strikeRow)
			streamText, streamStyle := renderStreamDuration(record)
			stream := renderCell(streamText, streamStyle, strikeRow)

			// Format row with fixed column widths
			sb.WriteString(fmt.Sprintf(
				"%s %s %s %s %s %s %s\n",
				timeCol,
				modelCol,
				requestInfo,
				initiator,
				status,
				duration,
				stream,
			))
		}
	}

	// Footer with scroll info.
	sb.WriteString(DimStyle.Render(fmt.Sprintf("\n  Showing %d of %d  %s", showing, len(allRecords), logsScrollHint)))

	// Debug status
	sb.WriteString("\n")
	sb.WriteString(v.renderDebugStatusLine())

	// Status message
	if v.state != nil && v.state.StatusView == ViewLogs && v.state.StatusMsg != "" {
		sb.WriteString("\n" + DimStyle.Render("  "+v.state.StatusMsg))
	}

	return sb.String()
}

func stylePrimaryColumns(
	record *monitor.RequestRecord,
	premiumByModel map[string]bool,
) (timeStyle, modelStyle *lipgloss.Style) {
	if shouldHighlightPrimaryColumns(record, premiumByModel) {
		return &logsPlainStyle, &logsPlainStyle
	}
	return &DimStyle, &DimStyle
}

func shouldHighlightPrimaryColumns(record *monitor.RequestRecord, premiumByModel map[string]bool) bool {
	if record.IsAgent {
		return false
	}
	return isKnownPremiumModel(record.Model, premiumByModel)
}

func renderInitiator(model string, isAgent bool) (string, *lipgloss.Style) {
	switch {
	case model == "-":
		return fmt.Sprintf("%-5s", "-"), &DimStyle
	case isAgent:
		return fmt.Sprintf("%-5s", "agent"), &DimStyle
	default:
		return fmt.Sprintf("%-5s", "user"), &logsPlainStyle
	}
}

func renderStatus(statusCode int) (string, *lipgloss.Style) {
	switch {
	case statusCode == 0:
		return fmt.Sprintf("%4s", "-"), &DimStyle
	case statusCode == monitor.StatusClientCanceled:
		return fmt.Sprintf("%4d", statusCode), &DimStyle
	case statusCode >= logsStatusErrorMin:
		return fmt.Sprintf("%4d", statusCode), &ErrorStyle
	default:
		return fmt.Sprintf("%4d", statusCode), &SuccessStyle
	}
}

func renderDuration(record *monitor.RequestRecord) (string, *lipgloss.Style) {
	if record.IsStream {
		if record.FirstResponseDuration > 0 {
			return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(record.FirstResponseDuration)), &logsPlainStyle
		}
		if record.StatusCode == 0 {
			elapsed := time.Since(record.Timestamp)
			return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(elapsed)), &DimStyle
		}
		return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(record.Duration)), &logsPlainStyle
	}

	if record.StatusCode == 0 {
		elapsed := time.Since(record.Timestamp)
		return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(elapsed)), &DimStyle
	}
	return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(record.Duration)), &logsPlainStyle
}

func renderStreamDuration(record *monitor.RequestRecord) (string, *lipgloss.Style) {
	if !record.IsStream {
		return fmt.Sprintf("%*s", logsDurationWidth, "-"), &DimStyle
	}
	if record.FirstResponseDuration <= 0 {
		return fmt.Sprintf("%*s", logsDurationWidth, "-"), &DimStyle
	}
	if record.Streaming {
		streamDuration := time.Since(record.Timestamp) - record.FirstResponseDuration
		if streamDuration < 0 {
			streamDuration = 0
		}
		return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(streamDuration)), &DimStyle
	}

	streamDuration := record.Duration - record.FirstResponseDuration
	if streamDuration < 0 {
		streamDuration = 0
	}
	return fmt.Sprintf("%*s", logsDurationWidth, FormatDuration(streamDuration)), &DimStyle
}

func (v *LogsView) renderDebugStatusLine() string {
	if v.debugEnabled && v.debugLogger != nil {
		logFile := v.debugLogger.FilePath()
		if logFile == "" {
			return "  Debug: " + SuccessStyle.Render("[on]")
		}
		return fmt.Sprintf("  Debug: %s %s",
			SuccessStyle.Render("[on]"),
			DimStyle.Render(logFile))
	}
	return DimStyle.Render("  Debug: [off]")
}

func isKnownPremiumModel(model string, premiumByModel map[string]bool) bool {
	if model == "" {
		return false
	}
	isPremium, ok := premiumByModel[model]
	return ok && isPremium
}

func shouldStrikeRequestRow(statusCode int) bool {
	return statusCode == monitor.StatusClientCanceled || statusCode == http.StatusGatewayTimeout
}

func renderCell(text string, baseStyle *lipgloss.Style, strike bool) string {
	if baseStyle == nil {
		baseStyle = &logsPlainStyle
	}
	if strike {
		style := StrikeStyle.Inherit(*baseStyle)
		return style.Render(text)
	}
	return baseStyle.Render(text)
}

// mergeActiveAndCompleted merges active and completed requests, sorted by timestamp (newest first).
func (v *LogsView) mergeActiveAndCompleted() []monitor.RequestRecord {
	active := v.state.Snapshot.ActiveRequests
	completed := v.state.Snapshot.RecentRequests

	merged := make([]monitor.RequestRecord, 0, len(active)+len(completed))
	merged = append(merged, active...)
	merged = append(merged, completed...)

	// Sort by timestamp descending (newest first)
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.After(merged[j].Timestamp)
	})

	return merged
}

func (v *LogsView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *LogsView) SetState(state *SharedState) {
	v.state = state
}

func (v *LogsView) SetDebugLogger(logger *monitor.DebugLogger) {
	v.debugLogger = logger
}

func (v *LogsView) SetDebugEnabled(enabled bool) {
	v.debugEnabled = enabled
}

func (v *LogsView) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if msg.Type == tea.KeyPgUp {
		return v.handlePageUp()
	}
	if msg.Type == tea.KeyPgDown {
		return v.handlePageDown()
	}

	switch msg.String() {
	case "up":
		return v.handleUp()
	case "down":
		return v.handleDown()
	case "pgup":
		return v.handlePageUp()
	case "pgdn", "pgdown":
		return v.handlePageDown()
	case "home":
		return v.handleHome()
	case "end":
		return v.handleEnd()
	case "g":
		return v.handleHome()
	case "G":
		return v.handleEnd()
	case "c":
		return v.handleClear()
	case "d":
		return v.handleDebugToggle()
	default:
		return false, nil
	}
}

func (v *LogsView) HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if !msg.Ctrl {
		return false, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return v.handleUp()
	case tea.MouseButtonWheelDown:
		return v.handleDown()
	case tea.MouseButtonNone,
		tea.MouseButtonLeft,
		tea.MouseButtonMiddle,
		tea.MouseButtonRight,
		tea.MouseButtonWheelLeft,
		tea.MouseButtonWheelRight,
		tea.MouseButtonBackward,
		tea.MouseButtonForward,
		tea.MouseButton10,
		tea.MouseButton11:
		return false, nil
	default:
		return false, nil
	}
}

func (v *LogsView) handleUp() (bool, tea.Cmd) {
	if v.offset > 0 {
		v.offset--
	}
	return true, nil
}

func (v *LogsView) handleDown() (bool, tea.Cmd) {
	allRecords := v.mergeActiveAndCompleted()
	visible := v.VisibleLines()
	maxOffset := len(allRecords) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.offset < maxOffset {
		v.offset++
	}
	return true, nil
}

func (v *LogsView) handlePageUp() (bool, tea.Cmd) {
	pageSize := v.VisibleLines()
	v.offset -= pageSize
	if v.offset < 0 {
		v.offset = 0
	}
	return true, nil
}

func (v *LogsView) handlePageDown() (bool, tea.Cmd) {
	pageSize := v.VisibleLines()
	allRecords := v.mergeActiveAndCompleted()
	maxOffset := len(allRecords) - pageSize
	if maxOffset < 0 {
		maxOffset = 0
	}
	v.offset += pageSize
	if v.offset > maxOffset {
		v.offset = maxOffset
	}
	return true, nil
}

func (v *LogsView) handleHome() (bool, tea.Cmd) {
	v.offset = 0
	return true, nil
}

func (v *LogsView) handleEnd() (bool, tea.Cmd) {
	allRecords := v.mergeActiveAndCompleted()
	visible := v.VisibleLines()
	maxOffset := len(allRecords) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	v.offset = maxOffset
	return true, nil
}

func (v *LogsView) handleClear() (bool, tea.Cmd) {
	v.offset = 0
	v.setLogsStatus("Logs cleared")
	return true, nil
}

func (v *LogsView) handleDebugToggle() (bool, tea.Cmd) {
	if v.debugLogger == nil {
		return true, nil
	}
	if v.debugLogger.DebugEnabled() {
		if err := v.debugLogger.DisableDebug(); err != nil {
			v.setLogsStatus(fmt.Sprintf("Error disabling debug: %v", err))
		} else {
			v.debugEnabled = false
			v.clearLogsStatus()
			v.SetDebugEnabled(false)
		}
		return true, nil
	}
	if err := v.debugLogger.EnableDebug(""); err != nil {
		v.setLogsStatus(fmt.Sprintf("Error enabling debug: %v", err))
		return true, nil
	}
	v.debugEnabled = true
	v.clearLogsStatus()
	v.SetDebugEnabled(true)
	return true, nil
}

func (v *LogsView) setLogsStatus(message string) {
	if v.state == nil {
		return
	}
	v.state.StatusMsg = message
	v.state.StatusView = ViewLogs
}

func (v *LogsView) clearLogsStatus() {
	v.setLogsStatus("")
}

func (v *LogsView) VisibleLines() int {
	// Account for header, table header, separator, footer, debug status.
	return ClampVisibleLines(v.height, logsFooterOffset, logsDefaultVisible)
}

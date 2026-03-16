package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/monitor"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestLogsView_AgentTimeAndModelAreDimmed(t *testing.T) {
	originalDim := DimStyle
	DimStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true)
	t.Cleanup(func() {
		DimStyle = originalDim
	})

	state := &SharedState{
		Models: []monitor.ModelInfo{
			{ID: "user-model", IsPremium: true},
			{ID: "agent-model", IsPremium: true},
		},
		Snapshot: monitor.Snapshot{
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 35, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "user-model",
					StatusCode:   200,
					Duration:     120 * time.Millisecond,
					IsAgent:      false,
				},
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 34, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "agent-model",
					StatusCode:   200,
					Duration:     180 * time.Millisecond,
					IsAgent:      true,
				},
			},
		},
	}

	view := NewLogsView()
	view.SetState(state)
	view.SetSize(120, 40)

	rendered := view.View()
	lines := strings.Split(rendered, "\n")

	var agentLine, userLine string
	for _, line := range lines {
		if strings.Contains(line, "agent-model") {
			agentLine = line
		}
		if strings.Contains(line, "user-model") {
			userLine = line
		}
	}

	if agentLine == "" {
		t.Fatalf("expected rendered logs to contain agent row")
	}
	if userLine == "" {
		t.Fatalf("expected rendered logs to contain user row")
	}

	if !strings.Contains(agentLine, "│12:34:56") {
		t.Fatalf("expected agent time to be dimmed, line=%q", agentLine)
	}
	if !strings.Contains(agentLine, "│agent-model") {
		t.Fatalf("expected agent model to be dimmed, line=%q", agentLine)
	}
	if strings.Contains(userLine, "│12:35:56") {
		t.Fatalf("expected user time to stay normal, line=%q", userLine)
	}
	if strings.Contains(userLine, "│user-model") {
		t.Fatalf("expected user model to stay normal, line=%q", userLine)
	}
}

func TestLogsView_ClientCanceledStatusUsesDimStyle(t *testing.T) {
	originalDim := DimStyle
	originalError := ErrorStyle
	originalSuccess := SuccessStyle
	makeTaggedStyle := func(tag string) lipgloss.Style {
		border := lipgloss.NormalBorder()
		border.Left = tag
		return lipgloss.NewStyle().BorderStyle(border).BorderLeft(true)
	}
	DimStyle = makeTaggedStyle("D")
	ErrorStyle = makeTaggedStyle("E")
	SuccessStyle = makeTaggedStyle("S")
	t.Cleanup(func() {
		DimStyle = originalDim
		ErrorStyle = originalError
		SuccessStyle = originalSuccess
	})

	state := &SharedState{
		Snapshot: monitor.Snapshot{
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 35, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "cancel-model",
					StatusCode:   monitor.StatusClientCanceled,
					Duration:     120 * time.Millisecond,
					IsAgent:      false,
				},
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 34, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "error-model",
					StatusCode:   500,
					Duration:     180 * time.Millisecond,
					IsAgent:      false,
				},
			},
		},
	}

	view := NewLogsView()
	view.SetState(state)
	view.SetSize(120, 40)

	rendered := view.View()
	lines := strings.Split(rendered, "\n")

	var canceledLine, errorLine string
	for _, line := range lines {
		if strings.Contains(line, "cancel-model") {
			canceledLine = line
		}
		if strings.Contains(line, "error-model") {
			errorLine = line
		}
	}

	if canceledLine == "" {
		t.Fatalf("expected rendered logs to contain canceled row")
	}
	if errorLine == "" {
		t.Fatalf("expected rendered logs to contain error row")
	}

	if !strings.Contains(canceledLine, "D 499") {
		t.Fatalf("expected status 499 to use dim style, line=%q", canceledLine)
	}
	if strings.Contains(canceledLine, "E 499") {
		t.Fatalf("expected status 499 not to use error style, line=%q", canceledLine)
	}
	if !strings.Contains(errorLine, "E 500") {
		t.Fatalf("expected status 500 to keep error style, line=%q", errorLine)
	}
}

func TestLogsView_CanceledAndTimeoutRowsUseStrikeStyle(t *testing.T) {
	originalDim := DimStyle
	originalError := ErrorStyle
	originalSuccess := SuccessStyle
	originalStrike := StrikeStyle
	makeTaggedStyle := func(tag string) lipgloss.Style {
		border := lipgloss.NormalBorder()
		border.Left = tag
		return lipgloss.NewStyle().BorderStyle(border).BorderLeft(true)
	}
	DimStyle = makeTaggedStyle("D")
	ErrorStyle = makeTaggedStyle("E")
	SuccessStyle = makeTaggedStyle("S")
	StrikeStyle = makeTaggedStyle("K")
	t.Cleanup(func() {
		DimStyle = originalDim
		ErrorStyle = originalError
		SuccessStyle = originalSuccess
		StrikeStyle = originalStrike
	})

	state := &SharedState{
		Snapshot: monitor.Snapshot{
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 37, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "cancel-model",
					StatusCode:   monitor.StatusClientCanceled,
					Duration:     100 * time.Millisecond,
				},
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 36, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "timeout-model",
					StatusCode:   504,
					Duration:     110 * time.Millisecond,
				},
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 35, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "error-model",
					StatusCode:   500,
					Duration:     120 * time.Millisecond,
				},
			},
		},
	}

	view := NewLogsView()
	view.SetState(state)
	view.SetSize(120, 40)

	rendered := view.View()
	lines := strings.Split(rendered, "\n")

	var canceledLine, timeoutLine, errorLine string
	for _, line := range lines {
		if strings.Contains(line, "cancel-model") {
			canceledLine = line
		}
		if strings.Contains(line, "timeout-model") {
			timeoutLine = line
		}
		if strings.Contains(line, "error-model") {
			errorLine = line
		}
	}

	if canceledLine == "" || timeoutLine == "" || errorLine == "" {
		t.Fatalf("expected rendered logs to contain canceled/timeout/error rows")
	}
	if !strings.Contains(canceledLine, "K") {
		t.Fatalf("expected canceled row to use strike style, line=%q", canceledLine)
	}
	if !strings.Contains(timeoutLine, "K") {
		t.Fatalf("expected timeout row to use strike style, line=%q", timeoutLine)
	}
	if strings.Contains(errorLine, "K") {
		t.Fatalf("expected regular 500 row not to use strike style, line=%q", errorLine)
	}
	if !strings.Contains(canceledLine, "499") {
		t.Fatalf("expected canceled row to include 499 status code, line=%q", canceledLine)
	}
	if !strings.Contains(timeoutLine, "504") {
		t.Fatalf("expected timeout row to include 504 status code, line=%q", timeoutLine)
	}
}

func TestLogsView_CanceledRowStrikeStyleDoesNotNestCellStyles(t *testing.T) {
	originalDim := DimStyle
	originalStrike := StrikeStyle
	makeTaggedStyle := func(tag string) lipgloss.Style {
		border := lipgloss.NormalBorder()
		border.Left = tag
		return lipgloss.NewStyle().BorderStyle(border).BorderLeft(true)
	}
	DimStyle = makeTaggedStyle("D")
	StrikeStyle = makeTaggedStyle("K")
	t.Cleanup(func() {
		DimStyle = originalDim
		StrikeStyle = originalStrike
	})

	state := &SharedState{
		Snapshot: monitor.Snapshot{
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 37, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "cancel-model",
					StatusCode:   monitor.StatusClientCanceled,
					Duration:     100 * time.Millisecond,
				},
			},
		},
	}

	view := NewLogsView()
	view.SetState(state)
	view.SetSize(120, 40)

	rendered := view.View()
	var canceledLine string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "cancel-model") {
			canceledLine = line
			break
		}
	}
	if canceledLine == "" {
		t.Fatalf("expected rendered logs to contain canceled row")
	}
	if !strings.Contains(canceledLine, "K") {
		t.Fatalf("expected canceled row to use strike style, line=%q", canceledLine)
	}
	if strings.Contains(canceledLine, "D") {
		t.Fatalf("expected canceled row strike render to avoid nested dim wrappers, line=%q", canceledLine)
	}
}

func TestLogsView_NonPremiumModelNameIsDimmed(t *testing.T) {
	originalDim := DimStyle
	DimStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true)
	t.Cleanup(func() {
		DimStyle = originalDim
	})

	state := &SharedState{
		Models: []monitor.ModelInfo{
			{ID: "premium-model", IsPremium: true},
			{ID: "free-model", IsPremium: false},
		},
		Snapshot: monitor.Snapshot{
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 35, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "premium-model",
					StatusCode:   200,
					Duration:     120 * time.Millisecond,
					IsAgent:      false,
				},
				{
					Timestamp:    time.Date(2026, 2, 24, 12, 34, 56, 0, time.UTC),
					Method:       "POST",
					Path:         "/v1/chat/completions",
					UpstreamPath: "/chat/completions",
					Model:        "free-model",
					StatusCode:   200,
					Duration:     180 * time.Millisecond,
					IsAgent:      false,
				},
			},
		},
	}

	view := NewLogsView()
	view.SetState(state)
	view.SetSize(120, 40)

	rendered := view.View()
	lines := strings.Split(rendered, "\n")

	var premiumLine, freeLine string
	for _, line := range lines {
		if strings.Contains(line, "premium-model") {
			premiumLine = line
		}
		if strings.Contains(line, "free-model") {
			freeLine = line
		}
	}

	if premiumLine == "" {
		t.Fatalf("expected rendered logs to contain premium row")
	}
	if freeLine == "" {
		t.Fatalf("expected rendered logs to contain non-premium row")
	}
	if strings.Contains(premiumLine, "│premium-model") {
		t.Fatalf("expected premium model name to stay normal, line=%q", premiumLine)
	}
	if strings.Contains(premiumLine, "│12:35:56") {
		t.Fatalf("expected premium user time to stay normal, line=%q", premiumLine)
	}
	if !strings.Contains(freeLine, "│free-model") {
		t.Fatalf("expected non-premium model name to be dimmed, line=%q", freeLine)
	}
	if !strings.Contains(freeLine, "│12:34:56") {
		t.Fatalf("expected non-premium time to be dimmed, line=%q", freeLine)
	}
}

func TestLogsView_MouseWheelDownWithCtrlScrolls(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12) // visible lines = 4
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	if view.offset != 0 {
		t.Fatalf("expected initial offset 0, got %d", view.offset)
	}

	handled, _ := view.HandleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Ctrl:   true,
	})
	if !handled {
		t.Fatalf("expected ctrl+wheel down to be handled")
	}
	if view.offset != 1 {
		t.Fatalf("expected offset 1 after ctrl+wheel down, got %d", view.offset)
	}
}

func TestLogsView_MouseWheelUpWithCtrlScrolls(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12) // visible lines = 4
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})
	view.offset = 2

	handled, _ := view.HandleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
		Ctrl:   true,
	})
	if !handled {
		t.Fatalf("expected ctrl+wheel up to be handled")
	}
	if view.offset != 1 {
		t.Fatalf("expected offset 1 after ctrl+wheel up, got %d", view.offset)
	}
}

func TestLogsView_MouseWheelWithoutCtrlIsIgnored(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12) // visible lines = 4
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	handled, _ := view.HandleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	if handled {
		t.Fatalf("expected wheel down without ctrl to be ignored")
	}
	if view.offset != 0 {
		t.Fatalf("expected offset to remain 0 without ctrl, got %d", view.offset)
	}
}

func TestLogsView_HomeEndAndGShortcutsJumpToBoundaries(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12)
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	maxOffset := len(records) - view.VisibleLines()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if view.offset != maxOffset {
		t.Fatalf("expected offset %d after End key, got %d", maxOffset, view.offset)
	}

	handled, _ = view.HandleKey(tea.KeyMsg{Type: tea.KeyHome})
	if !handled {
		t.Fatalf("expected Home key to be handled")
	}
	if view.offset != 0 {
		t.Fatalf("expected offset 0 after Home key, got %d", view.offset)
	}

	handled, _ = view.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if !handled {
		t.Fatalf("expected G key to be handled")
	}
	if view.offset != maxOffset {
		t.Fatalf("expected offset %d after G key, got %d", maxOffset, view.offset)
	}

	handled, _ = view.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if !handled {
		t.Fatalf("expected g key to be handled")
	}
	if view.offset != 0 {
		t.Fatalf("expected offset 0 after g key, got %d", view.offset)
	}
}

func TestLogsView_PgDownPagesDown(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12)
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	pageSize := view.VisibleLines()
	if pageSize <= 0 {
		t.Fatalf("expected positive page size, got %d", pageSize)
	}

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if !handled {
		t.Fatalf("expected PgDown key to be handled")
	}
	if view.offset != pageSize {
		t.Fatalf("expected offset %d after PgDown key, got %d", pageSize, view.offset)
	}
}

func TestLogsView_SmallHeightClampsVisibleLinesAndPaging(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 5)
	for i := range 5 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 3)
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	if got := view.VisibleLines(); got != 1 {
		t.Fatalf("expected VisibleLines to clamp to 1 for tiny height, got %d", got)
	}

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if !handled {
		t.Fatalf("expected PgDown key to be handled")
	}
	if view.offset != 1 {
		t.Fatalf("expected offset 1 after PgDown with clamped page size, got %d", view.offset)
	}
}

func TestLogsView_SpaceDoesNotPageDown(t *testing.T) {
	now := time.Now()
	records := make([]monitor.RequestRecord, 0, 20)
	for i := range 20 {
		records = append(records, monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      fmt.Sprintf("model-%02d", i),
			StatusCode: 200,
			Duration:   50 * time.Millisecond,
		})
	}

	view := NewLogsView()
	view.SetSize(120, 12)
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{RecentRequests: records},
	})

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if handled {
		t.Fatalf("expected Space key to be ignored")
	}
	if view.offset != 0 {
		t.Fatalf("expected offset 0 after Space key, got %d", view.offset)
	}
}

func TestLogsView_DebugKeyIgnored(t *testing.T) {
	view := NewLogsView()
	view.SetSize(120, 20)
	view.SetState(&SharedState{})

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if handled {
		t.Fatalf("expected debug key to be ignored")
	}
}

func TestLogsView_HeaderUsesTimestampDurationAndStream(t *testing.T) {
	view := NewLogsView()
	view.SetSize(140, 20)
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{},
	})

	rendered := view.View()
	if !strings.Contains(rendered, "Timestamp") {
		t.Fatalf("expected header to include Timestamp, got %q", rendered)
	}
	if !strings.Contains(rendered, "Duration") {
		t.Fatalf("expected header to include Duration, got %q", rendered)
	}
	if !strings.Contains(rendered, "Stream") {
		t.Fatalf("expected header to include Stream, got %q", rendered)
	}
}

func TestLogsView_RenderDurationAndStreamForSSE(t *testing.T) {
	streamingRecord := &monitor.RequestRecord{
		Timestamp:             time.Now().Add(-2 * time.Second),
		IsStream:              true,
		Streaming:             true,
		FirstResponseDuration: 120 * time.Millisecond,
		StatusCode:            200,
		Duration:              2 * time.Second,
	}
	durationText, _ := renderDuration(streamingRecord)
	if strings.TrimSpace(durationText) != "120ms" {
		t.Fatalf("expected streaming sse duration column to show first response time 120ms, got %q", durationText)
	}
	streamText, _ := renderStreamDuration(streamingRecord)
	trimmedStream := strings.TrimSpace(streamText)
	if trimmedStream == "" || trimmedStream == "-" {
		t.Fatalf("expected streaming sse stream column to show elapsed stream duration, got %q", streamText)
	}

	completedRecord := &monitor.RequestRecord{
		Timestamp:             time.Now().Add(-3 * time.Second),
		IsStream:              true,
		Streaming:             false,
		FirstResponseDuration: 300 * time.Millisecond,
		StatusCode:            200,
		Duration:              1300 * time.Millisecond,
	}
	completedStreamText, _ := renderStreamDuration(completedRecord)
	if strings.TrimSpace(completedStreamText) != "1.0s" {
		t.Fatalf("expected completed sse stream duration 1.0s, got %q", completedStreamText)
	}

	nonStreamRecord := &monitor.RequestRecord{
		Timestamp:  time.Now().Add(-1 * time.Second),
		IsStream:   false,
		StatusCode: 200,
		Duration:   500 * time.Millisecond,
	}
	nonStreamText, _ := renderStreamDuration(nonStreamRecord)
	if strings.TrimSpace(nonStreamText) != "-" {
		t.Fatalf("expected non-stream record stream column to be '-', got %q", nonStreamText)
	}
}

func TestLogsView_ActiveRequestBlinkIndicatorTogglesBySharedState(t *testing.T) {
	now := time.Now()
	view := NewLogsView()
	view.SetSize(120, 20)
	state := &SharedState{
		LogsBlinkOn: true,
		Snapshot: monitor.Snapshot{
			ActiveRequests: []monitor.RequestRecord{
				{
					Timestamp:  now,
					Method:     "POST",
					Path:       "/v1/responses",
					Model:      "active-model",
					StatusCode: 200,
				},
			},
			RecentRequests: []monitor.RequestRecord{
				{
					Timestamp:  now.Add(-time.Second),
					Method:     "POST",
					Path:       "/v1/responses",
					Model:      "done-model",
					StatusCode: 200,
					Duration:   120 * time.Millisecond,
				},
			},
		},
	}
	view.SetState(state)

	renderedOn := view.View()
	activeLineOn := findLineContaining(renderedOn, "active-model")
	if activeLineOn == "" {
		t.Fatalf("expected active row to be rendered")
	}
	if !strings.Contains(activeLineOn, "✦") {
		t.Fatalf("expected active row to show blink indicator when LogsBlinkOn=true, line=%q", activeLineOn)
	}

	doneLine := findLineContaining(renderedOn, "done-model")
	if doneLine == "" {
		t.Fatalf("expected completed row to be rendered")
	}
	if strings.Contains(doneLine, "✦") {
		t.Fatalf("expected completed row not to show blink indicator, line=%q", doneLine)
	}

	state.LogsBlinkOn = false
	renderedOff := view.View()
	activeLineOff := findLineContaining(renderedOff, "active-model")
	if activeLineOff == "" {
		t.Fatalf("expected active row to be rendered with blink off")
	}
	if strings.Contains(activeLineOff, "✦") {
		t.Fatalf("expected active row not to show blink indicator when LogsBlinkOn=false, line=%q", activeLineOff)
	}
}

func TestLogsView_ActiveSSERequestShowsBlinkIndicator(t *testing.T) {
	view := NewLogsView()
	view.SetSize(120, 20)
	view.SetState(&SharedState{
		LogsBlinkOn: true,
		Snapshot: monitor.Snapshot{
			ActiveRequests: []monitor.RequestRecord{
				{
					Timestamp:             time.Now().Add(-2 * time.Second),
					Method:                "POST",
					Path:                  "/v1/messages",
					Model:                 "active-sse-model",
					StatusCode:            200,
					IsStream:              true,
					Streaming:             true,
					FirstResponseDuration: 150 * time.Millisecond,
				},
			},
		},
	})

	rendered := view.View()
	sseLine := findLineContaining(rendered, "active-sse-model")
	if sseLine == "" {
		t.Fatalf("expected active sse row to be rendered")
	}
	if !strings.Contains(sseLine, "✦") {
		t.Fatalf("expected active sse row to show blink indicator, line=%q", sseLine)
	}
}

func findLineContaining(rendered, needle string) string {
	lines := strings.Split(rendered, "\n")
	for _, line := range lines {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

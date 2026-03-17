package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/core"
	"copilot-proxy/internal/models"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func makeTaggedStyle(tag string) lipgloss.Style {
	border := lipgloss.NormalBorder()
	border.Left = tag
	return lipgloss.NewStyle().BorderStyle(border).BorderLeft(true)
}

func statsRowForModel(rendered, model string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, model) {
			return line
		}
	}
	return ""
}

func TestStatsView_PremiumModelShowsUserAndAllCounts(t *testing.T) {
	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"gpt-4o": {
					Count:       8,
					AgentReqs:   2,
					Errors:      1,
					AgentErrors: 1,
					TotalTime:   4 * time.Second,
				},
			},
			TotalRequests: 8,
		},
		Models: []models.ModelInfo{
			{ID: "gpt-4o", IsPremium: true},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	rendered := view.View()
	if !strings.Contains(rendered, "Req(U/A)") || !strings.Contains(rendered, "Err(U/A)") {
		t.Fatalf("expected updated U/A headers, got:\n%s", rendered)
	}

	var row string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "gpt-4o") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("expected row for gpt-4o")
	}
	if !strings.Contains(row, "8/2") {
		t.Fatalf("expected premium requests to show user/agent as 8/2, row=%q", row)
	}
	if !strings.Contains(row, "1/1") {
		t.Fatalf("expected premium errors to show user/agent as 1/1, row=%q", row)
	}
}

func TestStatsView_AvgTimeUsesUserAndAgentRequestTotal(t *testing.T) {
	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"gpt-4o": {
					Count:       2,
					AgentReqs:   2,
					Errors:      0,
					AgentErrors: 0,
					TotalTime:   100 * time.Millisecond, // should not drive Avg Time now
				},
			},
			RecentRequests: []core.RequestRecord{
				{Timestamp: time.Now(), Model: "gpt-4o", StatusCode: 200, Duration: 100 * time.Millisecond, IsAgent: false},
				{Timestamp: time.Now(), Model: "gpt-4o", StatusCode: 200, Duration: 200 * time.Millisecond, IsAgent: false},
				{Timestamp: time.Now(), Model: "gpt-4o", StatusCode: 200, Duration: 300 * time.Millisecond, IsAgent: true},
				{Timestamp: time.Now(), Model: "gpt-4o", StatusCode: 200, Duration: 400 * time.Millisecond, IsAgent: true},
			},
			TotalRequests: 2,
		},
		Models: []models.ModelInfo{
			{ID: "gpt-4o", IsPremium: true},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	row := statsRowForModel(view.View(), "gpt-4o")
	if row == "" {
		t.Fatalf("expected row for gpt-4o")
	}
	if !strings.Contains(row, "250ms") {
		t.Fatalf("expected avg time 250ms with U+A denominator, row=%q", row)
	}
}

func TestStatsView_AvgTimeIncludesAgentDurations(t *testing.T) {
	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"gpt-4o-mini": {
					Count:       1,
					AgentReqs:   1,
					Errors:      0,
					AgentErrors: 0,
					TotalTime:   100 * time.Millisecond, // old logic would show 100ms
				},
			},
			RecentRequests: []core.RequestRecord{
				{Timestamp: time.Now(), Model: "gpt-4o-mini", StatusCode: 200, Duration: 100 * time.Millisecond, IsAgent: false},
				{Timestamp: time.Now(), Model: "gpt-4o-mini", StatusCode: 200, Duration: 900 * time.Millisecond, IsAgent: true},
			},
			TotalRequests: 1,
		},
		Models: []models.ModelInfo{
			{ID: "gpt-4o-mini", IsPremium: true},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	row := statsRowForModel(view.View(), "gpt-4o-mini")
	if row == "" {
		t.Fatalf("expected row for gpt-4o-mini")
	}
	if !strings.Contains(row, "500ms") {
		t.Fatalf("expected avg time 500ms including agent duration, row=%q", row)
	}
	if strings.Contains(row, "100ms") {
		t.Fatalf("expected avg time not to fall back to user-only timing, row=%q", row)
	}
}

func TestStatsView_AvgTimeForStreamUsesTotalDuration(t *testing.T) {
	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"stream-model": {
					Count:       1,
					AgentReqs:   0,
					Errors:      0,
					AgentErrors: 0,
					TotalTime:   300 * time.Millisecond, // old logic would show 300ms
				},
			},
			RecentRequests: []core.RequestRecord{
				{
					Timestamp:             time.Now(),
					Model:                 "stream-model",
					StatusCode:            200,
					Duration:              1800 * time.Millisecond,
					IsStream:              true,
					FirstResponseDuration: 300 * time.Millisecond,
					Streaming:             false,
				},
			},
			TotalRequests: 1,
		},
		Models: []models.ModelInfo{
			{ID: "stream-model", IsPremium: true},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	row := statsRowForModel(view.View(), "stream-model")
	if row == "" {
		t.Fatalf("expected row for stream-model")
	}
	if !strings.Contains(row, "1.8s") {
		t.Fatalf("expected stream avg time to use total duration 1.8s, row=%q", row)
	}
	if strings.Contains(row, "300ms") {
		t.Fatalf("expected stream avg time not to use first-response-only duration, row=%q", row)
	}
}

func TestStatsView_NonPremiumRowIsFullyDimmed(t *testing.T) {
	originalDim := DimStyle
	originalError := ErrorStyle
	DimStyle = makeTaggedStyle("D")
	ErrorStyle = makeTaggedStyle("E")
	t.Cleanup(func() {
		DimStyle = originalDim
		ErrorStyle = originalError
	})

	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"free-model": {
					Count:       3,
					AgentReqs:   2,
					Errors:      2,
					AgentErrors: 1,
					TotalTime:   3 * time.Second,
				},
			},
			TotalRequests: 3,
		},
		Models: []models.ModelInfo{
			{ID: "free-model", IsPremium: false},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	rendered := view.View()
	var row string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "free-model") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("expected row for free-model")
	}
	if !strings.Contains(row, "Dfree-model") {
		t.Fatalf("expected non-premium row to be dimmed, row=%q", row)
	}
	if !strings.Contains(row, "3/2") {
		t.Fatalf("expected non-premium request column to show user/agent split, row=%q", row)
	}
	if !strings.Contains(row, "2/1") {
		t.Fatalf("expected non-premium error column to show user/agent split, row=%q", row)
	}
	if strings.Contains(row, "E") {
		t.Fatalf("expected non-premium row to avoid error highlight and stay fully dimmed, row=%q", row)
	}
}

func TestStatsView_AgentOnlyModelShowsInList(t *testing.T) {
	state := &SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"agent-only-model": {
					Count:       0,
					AgentReqs:   4,
					Errors:      0,
					AgentErrors: 2,
				},
			},
		},
		Models: []models.ModelInfo{
			{ID: "agent-only-model", IsPremium: false},
		},
	}

	view := NewStatsView()
	view.SetState(state)

	rendered := view.View()
	if !strings.Contains(rendered, "agent-only-model") {
		t.Fatalf("expected agent-only model to be displayed, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "0/4") {
		t.Fatalf("expected agent-only model request split as 0/4, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "0/2") {
		t.Fatalf("expected agent-only model error split as 0/2, got:\n%s", rendered)
	}
}

func TestStatsView_HomeAndEndJumpListBoundaries(t *testing.T) {
	byModel := make(map[string]*core.ModelStats, 20)
	modelEntries := make([]models.ModelInfo, 0, 20)
	for i := range 20 {
		name := fmt.Sprintf("model-%02d", i)
		byModel[name] = &core.ModelStats{Count: int64(i + 1), TotalTime: time.Second}
		modelEntries = append(modelEntries, models.ModelInfo{ID: name, IsPremium: false})
	}

	view := NewStatsView()
	view.SetState(&SharedState{
		Snapshot: core.Snapshot{ByModel: byModel, TotalRequests: 20},
		Models:   modelEntries,
	})
	view.SetSize(120, 12)

	initial := view.View()
	if strings.Contains(initial, "model-19") {
		t.Fatalf("expected tail model to be off-screen before scrolling, got:\n%s", initial)
	}

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	endView := view.View()
	if !strings.Contains(endView, "model-19") {
		t.Fatalf("expected tail model to be visible after End key, got:\n%s", endView)
	}

	handled, _ = view.HandleKey(tea.KeyMsg{Type: tea.KeyHome})
	if !handled {
		t.Fatalf("expected Home key to be handled")
	}
	homeView := view.View()
	if !strings.Contains(homeView, "model-00") {
		t.Fatalf("expected head model to be visible after Home key, got:\n%s", homeView)
	}
}

func TestStatsView_SortsModelRowsByName(t *testing.T) {
	view := NewStatsView()
	view.SetState(&SharedState{
		Snapshot: core.Snapshot{
			ByModel: map[string]*core.ModelStats{
				"gpt-9": {Count: 1},
				"gpt-4": {Count: 100},
				"gpt-5": {Count: 10},
			},
		},
	})

	entries := view.sortedModelEntries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].name != "gpt-4" || entries[1].name != "gpt-5" || entries[2].name != "gpt-9" {
		t.Fatalf("expected name-sorted entries [gpt-4 gpt-5 gpt-9], got [%s %s %s]",
			entries[0].name, entries[1].name, entries[2].name)
	}
}

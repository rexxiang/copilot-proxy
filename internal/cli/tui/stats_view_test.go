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

func makeTaggedStyle(tag string) lipgloss.Style {
	border := lipgloss.NormalBorder()
	border.Left = tag
	return lipgloss.NewStyle().BorderStyle(border).BorderLeft(true)
}

func TestStatsView_PremiumModelShowsUserAndAllCounts(t *testing.T) {
	state := &SharedState{
		Snapshot: monitor.Snapshot{
			ByModel: map[string]*monitor.ModelStats{
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
		Models: []monitor.ModelInfo{
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
		Snapshot: monitor.Snapshot{
			ByModel: map[string]*monitor.ModelStats{
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
		Models: []monitor.ModelInfo{
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
		Snapshot: monitor.Snapshot{
			ByModel: map[string]*monitor.ModelStats{
				"agent-only-model": {
					Count:       0,
					AgentReqs:   4,
					Errors:      0,
					AgentErrors: 2,
				},
			},
		},
		Models: []monitor.ModelInfo{
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
	byModel := make(map[string]*monitor.ModelStats, 20)
	models := make([]monitor.ModelInfo, 0, 20)
	for i := range 20 {
		name := fmt.Sprintf("model-%02d", i)
		byModel[name] = &monitor.ModelStats{Count: int64(i + 1), TotalTime: time.Second}
		models = append(models, monitor.ModelInfo{ID: name, IsPremium: false})
	}

	view := NewStatsView()
	view.SetState(&SharedState{
		Snapshot: monitor.Snapshot{ByModel: byModel, TotalRequests: 20},
		Models:   models,
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
		Snapshot: monitor.Snapshot{
			ByModel: map[string]*monitor.ModelStats{
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

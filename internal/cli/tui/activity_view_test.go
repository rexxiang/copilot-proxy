package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActivityView_HomeAndEndJumpGranularityBoundaries(t *testing.T) {
	view := NewActivityView()
	view.granularity = granularityMonth

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	if view.granularity != granularityYear {
		t.Fatalf("expected End to jump to year granularity, got %v", view.granularity)
	}

	handled, _ = view.HandleKey(tea.KeyMsg{Type: tea.KeyHome})
	if !handled {
		t.Fatalf("expected Home key to be handled")
	}
	if view.granularity != granularityWeek {
		t.Fatalf("expected Home to jump to week granularity, got %v", view.granularity)
	}
}

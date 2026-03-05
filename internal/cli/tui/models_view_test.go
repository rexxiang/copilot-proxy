package tui

import (
	"fmt"
	"testing"

	"copilot-proxy/internal/monitor"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelsView_HomeAndEndJumpToBoundaries(t *testing.T) {
	models := make([]monitor.ModelInfo, 0, 20)
	for i := range 20 {
		models = append(models, monitor.ModelInfo{ID: fmt.Sprintf("model-%02d", i)})
	}

	view := NewModelsView()
	view.SetModels(models)
	view.SetSize(120, 12)

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	maxOffset := len(models) - view.VisibleLines()
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
}

func TestModelsView_SmallHeightClampsVisibleLinesAndPaging(t *testing.T) {
	models := make([]monitor.ModelInfo, 0, 5)
	for i := range 5 {
		models = append(models, monitor.ModelInfo{ID: fmt.Sprintf("model-%02d", i)})
	}

	view := NewModelsView()
	view.SetModels(models)
	view.SetSize(120, 3)

	if got := view.VisibleLines(); got != 1 {
		t.Fatalf("expected VisibleLines to clamp to 1 for tiny height, got %d", got)
	}

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	if view.offset != len(models)-1 {
		t.Fatalf("expected offset %d after End with clamped page size, got %d", len(models)-1, view.offset)
	}
}

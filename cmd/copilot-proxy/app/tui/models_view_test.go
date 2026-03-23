package tui

import (
	"fmt"
	"testing"

	models "copilot-proxy/internal/runtime/model"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelsView_HomeAndEndJumpToBoundaries(t *testing.T) {
	modelEntries := make([]models.ModelInfo, 0, 20)
	for i := range 20 {
		modelEntries = append(modelEntries, models.ModelInfo{ID: fmt.Sprintf("model-%02d", i)})
	}

	view := NewModelsView()
	view.SetModels(modelEntries)
	view.SetSize(120, 12)

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	maxOffset := len(modelEntries) - view.VisibleLines()
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
	modelEntries := make([]models.ModelInfo, 0, 5)
	for i := range 5 {
		modelEntries = append(modelEntries, models.ModelInfo{ID: fmt.Sprintf("model-%02d", i)})
	}

	view := NewModelsView()
	view.SetModels(modelEntries)
	view.SetSize(120, 3)

	if got := view.VisibleLines(); got != 1 {
		t.Fatalf("expected VisibleLines to clamp to 1 for tiny height, got %d", got)
	}

	handled, _ := view.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !handled {
		t.Fatalf("expected End key to be handled")
	}
	if view.offset != len(modelEntries)-1 {
		t.Fatalf("expected offset %d after End with clamped page size, got %d", len(modelEntries)-1, view.offset)
	}
}

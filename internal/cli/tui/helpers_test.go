package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderProgressBar_UsesUniformHeightGlyphs(t *testing.T) {
	originalProgressBarStyle := ProgressBarStyle
	originalDimStyle := DimStyle
	ProgressBarStyle = lipgloss.NewStyle()
	DimStyle = lipgloss.NewStyle()
	t.Cleanup(func() {
		ProgressBarStyle = originalProgressBarStyle
		DimStyle = originalDimStyle
	})

	bar := RenderProgressBar(75, 20)
	expected := strings.Repeat("■", 15) + strings.Repeat("□", 5)
	if bar != expected {
		t.Fatalf("expected uniform-height progress bar %q, got %q", expected, bar)
	}
}

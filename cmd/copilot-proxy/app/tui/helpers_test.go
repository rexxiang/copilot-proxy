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

func TestClampVisibleLines(t *testing.T) {
	tests := []struct {
		name     string
		height   int
		reserved int
		fallback int
		want     int
	}{
		{
			name:     "uses fallback when height is zero",
			height:   0,
			reserved: 8,
			fallback: 10,
			want:     10,
		},
		{
			name:     "normalizes negative fallback",
			height:   -1,
			reserved: 8,
			fallback: -5,
			want:     0,
		},
		{
			name:     "clamps tiny positive height to one row",
			height:   3,
			reserved: 8,
			fallback: 10,
			want:     1,
		},
		{
			name:     "returns computed visible rows",
			height:   20,
			reserved: 8,
			fallback: 10,
			want:     12,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClampVisibleLines(tc.height, tc.reserved, tc.fallback)
			if got != tc.want {
				t.Fatalf(
					"ClampVisibleLines(height=%d, reserved=%d, fallback=%d) = %d, want %d",
					tc.height, tc.reserved, tc.fallback, got, tc.want,
				)
			}
		})
	}
}

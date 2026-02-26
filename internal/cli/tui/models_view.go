package tui

import (
	"fmt"
	"sort"
	"strings"

	"copilot-proxy/internal/monitor"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type ModelsView struct {
	width  int
	height int
	state  *SharedState
	offset int
	models []monitor.ModelInfo
}

const (
	modelsHeaderSeparatorWidth = 70
	modelsIDWidth              = 26
	modelsDefaultVisibleOffset = 10
)

func NewModelsView() *ModelsView {
	return &ModelsView{}
}

//goland:noinspection GoUnusedParameter
func (v *ModelsView) Update(msg tea.Msg) (ViewComponent, tea.Cmd) {
	return v, nil
}

func (v *ModelsView) View() string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(TableHeaderStyle.Render(fmt.Sprintf("%-26s %-6s %-20s %s", "Model ID", "Multi", "Ctx", "Endpoints")))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", modelsHeaderSeparatorWidth))
	sb.WriteString("\n")

	if len(v.models) == 0 {
		sb.WriteString(DimStyle.Render("  Loading models... (press 'r' to refresh)\n"))
		return sb.String()
	}

	// Sort models by ID
	sortedModels := make([]monitor.ModelInfo, len(v.models))
	copy(sortedModels, v.models)
	sort.Slice(sortedModels, func(i, j int) bool {
		return sortedModels[i].ID < sortedModels[j].ID
	})

	visible := v.VisibleLines()
	if visible < 1 {
		visible = 10
	}

	start := v.offset
	end := start + visible
	if end > len(sortedModels) {
		end = len(sortedModels)
	}

	for i := start; i < end; i++ {
		model := sortedModels[i]
		endpoints := FormatEndpoints(model.Endpoints)

		// Format multiplier from billing.multiplier
		// Examples: 0x, 0.33x, 1x, 3x, N/A
		var multiText string
		var multiStyled string
		switch {
		case model.Multiplier > 0:
			// Use integer format if whole number, otherwise show decimal
			if model.Multiplier == float64(int(model.Multiplier)) {
				multiText = fmt.Sprintf("%dx", int(model.Multiplier))
			} else {
				multiText = fmt.Sprintf("%.2gx", model.Multiplier)
			}
			if model.IsPremium {
				multiStyled = PremiumStyle.Render(fmt.Sprintf("%-6s", multiText))
			} else {
				multiStyled = fmt.Sprintf("%-6s", multiText)
			}
		case model.Multiplier == 0:
			// Multiplier is 0, show "0x"
			multiText = "0x"
			multiStyled = fmt.Sprintf("%-6s", multiText)
		default:
			// Unknown/not set, show N/A
			multiText = "N/A"
			multiStyled = DimStyle.Render(fmt.Sprintf("%-6s", multiText))
		}

		// Format context summary (e.g., 128K, ↑ 128K ↓ 16K)
		ctx := fmt.Sprintf("%-20s", FormatContextSummary(model.ContextWindow, model.MaxPromptTokens, model.MaxOutputTokens))

		id := model.ID
		if model.Preview {
			id += " *"
		}

		sb.WriteString(fmt.Sprintf("%-26s %s %s %s\n",
			Truncate(id, modelsIDWidth),
			multiStyled,
			ctx,
			endpoints))
	}

	if len(sortedModels) > visible {
		sb.WriteString(DimStyle.Render(fmt.Sprintf("\n  Showing %d-%d of %d (↑↓ to scroll)", start+1, end, len(sortedModels))))
	}

	sb.WriteString(DimStyle.Render("\n\n  C=/chat/completions R=/responses M=/v1/messages"))

	// Status message
	if v.state != nil && v.state.StatusView == ViewModels && v.state.StatusMsg != "" {
		sb.WriteString("\n" + DimStyle.Render(v.state.StatusMsg))
	}

	return sb.String()
}

func (v *ModelsView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *ModelsView) SetState(state *SharedState) {
	v.state = state
}

func (v *ModelsView) SetModels(models []monitor.ModelInfo) {
	v.models = models
}

func (v *ModelsView) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if key.Matches(msg, key.NewBinding(key.WithKeys("r"))) {
		// Refresh logic would be handled by parent
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))) {
		if v.offset > 0 {
			v.offset--
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))) {
		maxOffset := len(v.models) - v.VisibleLines()
		if maxOffset < 0 {
			maxOffset = 0
		}
		if v.offset < maxOffset {
			v.offset++
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))) {
		pageSize := v.VisibleLines()
		v.offset -= pageSize
		if v.offset < 0 {
			v.offset = 0
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("pgdn"))) {
		pageSize := v.VisibleLines()
		maxOffset := len(v.models) - pageSize
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.offset += pageSize
		if v.offset > maxOffset {
			v.offset = maxOffset
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("home"))) {
		v.offset = 0
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("end"))) {
		maxOffset := len(v.models) - v.VisibleLines()
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.offset = maxOffset
		return true, nil
	}
	return false, nil
}

func (v *ModelsView) VisibleLines() int {
	if v.height <= 0 {
		return 0
	}
	return v.height - modelsDefaultVisibleOffset
}

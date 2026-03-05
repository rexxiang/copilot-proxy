package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"copilot-proxy/internal/monitor"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type StatsView struct {
	width  int
	height int
	state  *SharedState
	offset int
}

const (
	statsPercentBase         = 100.0
	statsProgressBarWidth    = 20
	statsHeaderSeparatorSize = 59
	statsModelNameWidth      = 20
	statsReservedLines       = 8
	statsDefaultVisibleRows  = 5
)

type statsModelEntry struct {
	name  string
	stats *monitor.ModelStats
}

func NewStatsView() *StatsView {
	return &StatsView{}
}

//goland:noinspection GoUnusedParameter
func (v *StatsView) Update(msg tea.Msg) (ViewComponent, tea.Cmd) {
	return v, nil
}

func (v *StatsView) View() string {
	var sb strings.Builder

	v.renderAccountAndSubscription(&sb)
	sb.WriteString("\n")
	v.renderTableHeader(&sb)

	entries := v.sortedModelEntries()
	premiumByModel := v.premiumByModel()
	start, end, visible := v.visibleWindow(len(entries))
	v.renderModelRows(&sb, entries, premiumByModel, start, end)
	v.renderRowsFooter(&sb, len(entries), start, end, visible)

	v.renderSummary(&sb)
	v.renderStatusMessage(&sb)

	return sb.String()
}

func (v *StatsView) renderAccountAndSubscription(sb *strings.Builder) {
	if sb == nil || v == nil || v.state == nil {
		return
	}
	if v.state.AuthConfig != nil {
		account, _, _ := v.state.AuthConfig.DefaultAccount()
		if account.User != "" {
			_, _ = fmt.Fprintf(sb, "\nAccount: %s\n", account.User)
		}
	}

	if v.state.UserInfo != nil {
		plan := v.state.UserInfo.Plan
		if v.state.UserInfo.Organization != "" {
			plan = fmt.Sprintf("%s (%s)", plan, v.state.UserInfo.Organization)
		}
		_, _ = fmt.Fprintf(sb, "Subscription: %s\n", plan)

		if v.state.UserInfo.Quota.Unlimited {
			sb.WriteString("Premium Interactions: Unlimited\n")
			return
		}

		used := v.state.UserInfo.Quota.Entitlement - v.state.UserInfo.Quota.Remaining
		total := v.state.UserInfo.Quota.Entitlement
		pct := statsPercentBase - v.state.UserInfo.Quota.PercentRemaining
		bar := RenderProgressBar(pct, statsProgressBarWidth)
		_, _ = fmt.Fprintf(sb, "Premium Interactions: %s %d/%d (%.1f%%)\n", bar, used, total, pct)
		return
	}

	sb.WriteString("\n" + DimStyle.Render("Loading subscription info... (press 'r' to refresh)") + "\n")
}

func (v *StatsView) renderTableHeader(sb *strings.Builder) {
	if sb == nil {
		return
	}
	sb.WriteString(TableHeaderStyle.Render(fmt.Sprintf("%-20s %12s %14s %10s", "Model", "Req(U/A)", "Err(U/A)", "Avg Time")))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("-", statsHeaderSeparatorSize))
	sb.WriteString("\n")
}

func (v *StatsView) sortedModelEntries() []statsModelEntry {
	if v == nil || v.state == nil {
		return nil
	}
	entries := make([]statsModelEntry, 0, len(v.state.Snapshot.ByModel))
	for name, stats := range v.state.Snapshot.ByModel {
		if name == "" {
			continue
		}
		entries = append(entries, statsModelEntry{name: name, stats: stats})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
	return entries
}

func (v *StatsView) premiumByModel() map[string]bool {
	premium := make(map[string]bool)
	if v == nil || v.state == nil {
		return premium
	}
	premium = make(map[string]bool, len(v.state.Models))
	for i := range v.state.Models {
		model := &v.state.Models[i]
		premium[model.ID] = model.IsPremium
	}
	return premium
}

func (v *StatsView) visibleWindow(totalRows int) (start, end, visible int) {
	visible = v.VisibleLines()
	if visible < 1 {
		visible = statsDefaultVisibleRows
	}
	start = v.offset
	maxOffset := totalRows - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if start > maxOffset {
		start = maxOffset
		v.offset = maxOffset
	}
	end = start + visible
	if end > totalRows {
		end = totalRows
	}
	return start, end, visible
}

func (v *StatsView) renderModelRows(
	sb *strings.Builder,
	entries []statsModelEntry,
	premiumByModel map[string]bool,
	start, end int,
) {
	if sb == nil {
		return
	}
	for _, entry := range entries[start:end] {
		isPremium := premiumByModel[entry.name]
		userRequests := entry.stats.Count
		agentRequests := entry.stats.AgentReqs
		userErrors := entry.stats.Errors
		agentErrors := entry.stats.AgentErrors
		allErrors := userErrors + agentErrors

		avgTime := time.Duration(0)
		if userRequests > 0 {
			avgTime = entry.stats.TotalTime / time.Duration(userRequests)
		}

		reqText := fmt.Sprintf("%d/%d", userRequests, agentRequests)
		errText := fmt.Sprintf("%d/%d", userErrors, agentErrors)

		modelCol := fmt.Sprintf("%-20s", Truncate(entry.name, statsModelNameWidth))
		reqCol := fmt.Sprintf("%12s", reqText)
		errCol := fmt.Sprintf("%14s", errText)
		avgCol := fmt.Sprintf("%10s", FormatDuration(avgTime))

		if isPremium {
			if allErrors > 0 {
				errCol = ErrorStyle.Render(errCol)
			}
			_, _ = fmt.Fprintf(sb, "%s %s %s %s\n", modelCol, reqCol, errCol, avgCol)
			continue
		}

		row := fmt.Sprintf("%s %s %s %s", modelCol, reqCol, errCol, avgCol)
		sb.WriteString(DimStyle.Render(row))
		sb.WriteString("\n")
	}
}

func (v *StatsView) renderRowsFooter(sb *strings.Builder, totalRows, start, end, visible int) {
	if sb == nil {
		return
	}
	if totalRows == 0 {
		sb.WriteString(DimStyle.Render("  No requests yet\n"))
		return
	}
	if totalRows > visible {
		sb.WriteString(DimStyle.Render(
			fmt.Sprintf("  Showing %d-%d of %d  ↑↓ PgUp/PgDn Home/End\n", start+1, end, totalRows),
		))
	}
}

func (v *StatsView) renderSummary(sb *strings.Builder) {
	if sb == nil || v == nil || v.state == nil {
		return
	}
	sb.WriteString("\n")
	var totalErrors int64
	for _, stats := range v.state.Snapshot.ByModel {
		totalErrors += stats.Errors
	}
	errPct := float64(0)
	if v.state.Snapshot.TotalRequests > 0 {
		errPct = float64(totalErrors) / float64(v.state.Snapshot.TotalRequests) * statsPercentBase
	}
	_, _ = fmt.Fprintf(sb, "Total: %d requests | %d errors (%.1f%%)\n",
		v.state.Snapshot.TotalRequests, totalErrors, errPct)
}

func (v *StatsView) renderStatusMessage(sb *strings.Builder) {
	if sb == nil || v == nil || v.state == nil {
		return
	}
	if v.state.StatusView == ViewStats && v.state.StatusMsg != "" {
		sb.WriteString("\n" + DimStyle.Render(v.state.StatusMsg) + "\n")
	}
}

func (v *StatsView) SetSize(width, height int) {
	v.width = width
	v.height = height
}

func (v *StatsView) SetState(state *SharedState) {
	v.state = state
}

func (v *StatsView) HandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if key.Matches(msg, key.NewBinding(key.WithKeys("r"))) {
		// Refresh logic is handled by parent
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))) {
		if v.offset > 0 {
			v.offset--
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))) {
		maxOffset := v.maxOffset()
		if v.offset < maxOffset {
			v.offset++
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))) {
		v.offset -= v.VisibleLines()
		if v.offset < 0 {
			v.offset = 0
		}
		return true, nil
	}
	if key.Matches(msg, key.NewBinding(key.WithKeys("pgdn"))) {
		v.offset += v.VisibleLines()
		maxOffset := v.maxOffset()
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
		v.offset = v.maxOffset()
		return true, nil
	}
	return false, nil
}

func (v *StatsView) VisibleLines() int {
	return ClampVisibleLines(v.height, statsReservedLines, 0)
}

func (v *StatsView) maxOffset() int {
	if v == nil || v.state == nil {
		return 0
	}
	totalRows := 0
	for modelName := range v.state.Snapshot.ByModel {
		if modelName == "" {
			continue
		}
		totalRows++
	}
	maxOffset := totalRows - v.VisibleLines()
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

package app

import (
	"fmt"
	"strings"
	"time"

	"copilot-proxy/cmd/copilot-proxy/app/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *MonitorModel) handlePrevView() (tea.Model, tea.Cmd) {
	if m.state > tui.ViewStats {
		m.state--
	} else {
		m.state = tui.ViewLogs
	}
	cmd := m.handleViewEnter()
	return m, cmd
}

func (m *MonitorModel) handleNextView() (tea.Model, tea.Cmd) {
	if m.state < tui.ViewLogs {
		m.state++
	} else {
		m.state = tui.ViewStats
	}
	cmd := m.handleViewEnter()
	return m, cmd
}

func (m *MonitorModel) handleDirectView(state tui.ViewState) (tea.Model, tea.Cmd) {
	m.state = state
	cmd := m.handleViewEnter()
	return m, cmd
}

func (m *MonitorModel) handleCurrentViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, cmd := m.currentViewHandleKey(msg); handled {
		return m, cmd
	}
	return m, nil
}

func (m *MonitorModel) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !isMouseWheelButton(msg.Button) {
		return m, nil
	}

	if !msg.Ctrl {
		return m, nil
	}

	if handled, cmd := m.currentViewHandleMouse(msg); handled {
		return m, cmd
	}
	return m, nil
}

func isMouseWheelButton(button tea.MouseButton) bool {
	return button == tea.MouseButtonWheelUp ||
		button == tea.MouseButtonWheelDown ||
		button == tea.MouseButtonWheelLeft ||
		button == tea.MouseButtonWheelRight
}

func (m *MonitorModel) handleClearLogsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		if m.statsService != nil {
			m.statsService.Reset()
			m.snapshot = m.statsService.MonitorSnapshot()
			m.sharedState.Snapshot = m.snapshot
		}
		m.setStatus(tui.ViewStats, "Stats counters cleared")
		return m, nil
	case tui.ViewModels:
		return m.handleCurrentViewKey(msg)
	case tui.ViewLogs:
		if m.statsService != nil {
			m.statsService.Reset()
			m.snapshot = m.statsService.MonitorSnapshot()
			m.sharedState.Snapshot = m.snapshot
		}
	}
	return m.handleCurrentViewKey(msg)
}

func (m *MonitorModel) handleRefresh() (tea.Model, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		return m, m.enqueueUserInfoRefresh(userInfoRefreshSourceManual)
	case tui.ViewModels:
		m.loading = true
		m.setStatus(tui.ViewModels, "Refreshing models...")
		cmd := m.loadModelsCmd()
		return m, cmd
	case tui.ViewLogs:
		return m, nil
	default:
		return m, nil
	}
}

func (m *MonitorModel) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.applyViewSizes()
	m.sharedState.Width = msg.Width
	m.sharedState.Height = msg.Height
}

func (m *MonitorModel) handleModelsLoaded(msg modelsLoadedMsg) {
	m.loading = false
	if msg.err == nil {
		m.sharedState.Models = msg.models
		m.setStatus(tui.ViewModels, fmt.Sprintf("Loaded %d models", len(msg.models)))
		m.lastRefresh = time.Now()
		m.loadedModels = true
		m.modelsView.SetModels(msg.models)
		return
	}
	m.setStatus(tui.ViewModels, fmt.Sprintf("Models: %v", msg.err))
}

func (m *MonitorModel) handleViewEnter() tea.Cmd {
	switch m.state {
	case tui.ViewStats:
		if !m.loadedUserInfo {
			return m.beginUserInfoRefresh()
		}
	case tui.ViewModels:
		if !m.loadedModels {
			m.loading = true
			m.setStatus(tui.ViewModels, "Refreshing models...")
			return m.loadModelsCmd()
		}
	case tui.ViewLogs:
		return nil
	}
	return nil
}

func (m *MonitorModel) currentViewHandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		return m.statsView.HandleKey(msg)
	case tui.ViewModels:
		return m.modelsView.HandleKey(msg)
	case tui.ViewLogs:
		return m.logsView.HandleKey(msg)
	default:
		return false, nil
	}
}

func (m *MonitorModel) currentViewHandleMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		return false, nil
	case tui.ViewModels:
		return false, nil
	case tui.ViewLogs:
		return m.logsView.HandleMouse(msg)
	default:
		return false, nil
	}
}

// View renders the current view.
func (m *MonitorModel) View() string {
	if m.quitting {
		return ""
	}

	m.applyViewSizes()

	var content string
	switch m.state {
	case tui.ViewStats:
		content = m.statsView.View()
	case tui.ViewModels:
		content = m.modelsView.View()
	case tui.ViewLogs:
		content = m.logsView.View()
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	baseView := lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
	if m.configModal != nil && m.configModal.IsOpen() {
		return m.configModal.Overlay(baseView, m.width, m.height)
	}
	if m.accountModal != nil && m.accountModal.IsOpen() {
		return m.accountModal.Overlay(baseView, m.width, m.height)
	}
	return baseView
}

func (m *MonitorModel) renderHeader() string {
	title := tui.TitleStyle.Render("Copilot Proxy")

	tabs := []string{}
	tabNames := []string{"Stats", "Models", "Logs"}
	viewOrder := []tui.ViewState{tui.ViewStats, tui.ViewModels, tui.ViewLogs}
	for i, name := range tabNames {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if viewOrder[i] == m.state {
			tabs = append(tabs, tui.SelectedTabStyle.Render(" "+label+" "))
		} else {
			tabs = append(tabs, tui.TabStyle.Render(" "+label+" "))
		}
	}

	tabLine := strings.Join(tabs, " ")
	addr := tui.DimStyle.Render(m.serverAddr)

	headerContent := fmt.Sprintf("%s  %s  %s", title, tabLine, addr)
	if m.width > 0 {
		return tui.HeaderStyle.Width(m.width).Render(headerContent)
	}
	return tui.HeaderStyle.Render(headerContent)
}

func (m *MonitorModel) renderFooter() string {
	helpKeys := m.keys
	m.resetFooterKeyVisibility(&helpKeys)
	m.applyFooterKeyOverrides(&helpKeys)
	helpView := m.help.View(&helpKeys)
	return tui.DimStyle.Render(helpView)
}

func (m *MonitorModel) applyFooterKeyOverrides(helpKeys *monitorKeyMap) {
	if helpKeys == nil {
		return
	}
	switch m.state {
	case tui.ViewStats:
		m.footerKeysForStats(helpKeys)
	case tui.ViewModels:
		m.footerKeysForModels(helpKeys)
	case tui.ViewLogs:
		m.footerKeysForLogs(helpKeys)
	default:
		m.footerKeysForStats(helpKeys)
	}
}

func (m *MonitorModel) footerKeysForStats(helpKeys *monitorKeyMap) {
	helpKeys.accounts.SetEnabled(true)
}

func (m *MonitorModel) footerKeysForModels(_ *monitorKeyMap) {}

func (m *MonitorModel) footerKeysForLogs(_ *monitorKeyMap) {}

func (m *MonitorModel) resetFooterKeyVisibility(helpKeys *monitorKeyMap) {
	if helpKeys == nil {
		return
	}
	helpKeys.accounts.SetEnabled(false)
}

func (m *MonitorModel) applyViewSizes() {
	contentHeight := m.calculateContentHeight()
	m.statsView.SetSize(m.width, contentHeight)
	m.modelsView.SetSize(m.width, contentHeight)
	m.logsView.SetSize(m.width, contentHeight)
}

func (m *MonitorModel) calculateContentHeight() int {
	if m.height <= 0 {
		return 0
	}
	headerHeight := lipgloss.Height(m.renderHeader())
	footerHeight := lipgloss.Height(m.renderFooter())
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 1 {
		return 1
	}
	return contentHeight
}

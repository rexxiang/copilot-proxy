package app

import (
	"fmt"
	"time"

	"copilot-proxy/cmd/copilot-proxy/app/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *MonitorModel) beginUserInfoRefresh() tea.Cmd {
	return m.startUserInfoRefreshIfNeeded()
}

func (m *MonitorModel) beginUserInfoRefreshDeferred() tea.Cmd {
	if m.userInfoQueue.State().InFlight {
		m.userInfoRefreshAfterInFlight = true
		return nil
	}
	return m.startUserInfoRefreshIfNeeded()
}

func (m *MonitorModel) enqueueUserInfoRefresh(source userInfoRefreshSource) tea.Cmd {
	result := m.userInfoQueue.Trigger()
	if source == userInfoRefreshSourceManual {
		m.userInfoForceRefresh = true
	}
	if !result.Schedule {
		if source == userInfoRefreshSourceManual && m.userInfoQueue.State().InFlight {
			m.userInfoRefreshAfterInFlight = true
			m.setStatus(tui.ViewStats, "User info refresh queued after current request")
		}
		return nil
	}
	if source == userInfoRefreshSourceManual {
		m.setStatus(tui.ViewStats, "Queued user info refresh (3s)")
	}
	return scheduleUserInfoDue(result.Seq, result.Delay)
}

func (m *MonitorModel) startUserInfoRefreshIfNeeded() tea.Cmd {
	if m.userInfoQueue.State().InFlight {
		return nil
	}
	m.userInfoQueue.MarkStarted()
	m.loading = true
	m.setStatus(tui.ViewStats, "Refreshing user info...")
	force := m.userInfoForceRefresh
	m.userInfoForceRefresh = false
	return m.loadUserInfoCmd(force)
}

func scheduleUserInfoDue(seq int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return userInfoRefreshDueMsg{seq: seq}
	})
}

func (m *MonitorModel) handleUserInfoRefreshDue(msg userInfoRefreshDueMsg) (tea.Model, tea.Cmd) {
	if !m.userInfoQueue.AcceptDue(msg.seq) {
		return m, nil
	}
	return m, m.startUserInfoRefreshIfNeeded()
}

func (m *MonitorModel) handleTick() (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{tickCmd()}
	if m.sharedState != nil {
		m.sharedState.LogsBlinkOn = !m.sharedState.LogsBlinkOn
	}
	if m.statsService != nil {
		m.snapshot = m.statsService.MonitorSnapshot()
		m.sharedState.Snapshot = m.snapshot
		premiumSet := premiumModelSet(m.sharedState.Models)
		if len(premiumSet) > 0 && m.premiumDetector.HasNewEligible(m.snapshot, premiumSet) {
			if cmd := m.enqueueUserInfoRefresh(userInfoRefreshSourceAgentPremium); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *MonitorModel) handleUserInfoLoaded(msg userInfoLoadedMsg) (tea.Model, tea.Cmd) {
	m.userInfoQueue.MarkFinished()
	if m.userInfoRefreshAfterInFlight {
		m.userInfoRefreshAfterInFlight = false
		return m, m.startUserInfoRefreshIfNeeded()
	}
	m.loading = false
	if msg.err == nil {
		m.sharedState.UserInfo = msg.info
		m.loadedUserInfo = msg.info != nil
		m.setStatus(tui.ViewStats, "Subscription info updated")
		return m, nil
	}
	m.setStatus(tui.ViewStats, fmt.Sprintf("User: %v", msg.err))
	return m, nil
}

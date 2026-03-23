package app

import (
	"fmt"

	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/cmd/copilot-proxy/app/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *MonitorModel) handleOpenSettingsModal() tea.Model {
	settings, err := m.loadSettings()
	if err != nil {
		m.setStatus(m.state, fmt.Sprintf("Settings: %v", err))
		return m
	}
	m.currentSettings = settings
	if m.configModal == nil {
		m.configModal = tui.NewConfigModal()
	}
	if err := m.configModal.Open(&settings); err != nil {
		m.setStatus(m.state, fmt.Sprintf("Settings modal: %v", err))
		return m
	}
	return m
}

func (m *MonitorModel) handleConfigModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := m.configModal.HandleKey(msg)
	switch action {
	case tui.ModalActionNone:
		return m, nil
	case tui.ModalActionClose:
		m.configModal.Close()
		return m, nil
	case tui.ModalActionSave:
		candidate, err := m.configModal.BuildCandidate(&m.currentSettings)
		if err != nil {
			m.configModal.SetError(err.Error())
			return m, nil
		}
		cmd := m.applySettingsCmd(&candidate)
		return m, cmd
	default:
		return m, nil
	}
}

func (m *MonitorModel) applySettingsCmd(candidate *appsettings.Settings) tea.Cmd {
	return func() tea.Msg {
		if candidate == nil {
			return settingsAppliedMsg{
				settings: nil,
				err:      errSettingsApplyResultNil,
			}
		}
		settings, err := m.applySettings(*candidate)
		applied := settings
		return settingsAppliedMsg{
			settings: &applied,
			err:      err,
		}
	}
}

func (m *MonitorModel) handleSettingsApplied(msg *settingsAppliedMsg) {
	if msg == nil {
		m.setStatus(m.state, fmt.Sprintf("Settings: %v", errSettingsApplyResultNil))
		return
	}
	if msg.err != nil {
		if m.configModal != nil {
			m.configModal.SetError(msg.err.Error())
		}
		m.setStatus(m.state, fmt.Sprintf("Settings: %v", msg.err))
		return
	}
	if msg.settings == nil {
		if m.configModal != nil {
			m.configModal.SetError(errSettingsApplyResultNil.Error())
		}
		m.setStatus(m.state, fmt.Sprintf("Settings: %v", errSettingsApplyResultNil))
		return
	}
	m.currentSettings = *msg.settings
	if m.configModal != nil {
		m.configModal.Close()
	}
	m.setStatus(m.state, "Settings saved and applied")
}

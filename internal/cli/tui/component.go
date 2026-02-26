package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"
)

// ViewState represents the current active view.
type ViewState int

const (
	ViewStats ViewState = iota
	ViewActivity
	ViewModels
	ViewLogs
)

type ViewComponent interface {
	Update(msg tea.Msg) (ViewComponent, tea.Cmd)
	View() string
	SetSize(width, height int)
	SetState(state *SharedState)
	HandleKey(msg tea.KeyMsg) (bool, tea.Cmd)
	VisibleLines() int
}

type SharedState struct {
	Snapshot   monitor.Snapshot
	Models     []monitor.ModelInfo
	UserInfo   *monitor.UserInfo
	AuthConfig *config.AuthConfig
	Width      int
	Height     int
	StatusMsg  string
	StatusView ViewState
}

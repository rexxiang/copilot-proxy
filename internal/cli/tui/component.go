package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"copilot-proxy/internal/config"
	"copilot-proxy/internal/core"
	"copilot-proxy/internal/models"
)

// ViewState represents the current active view.
type ViewState int

const (
	ViewStats ViewState = iota
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
	Snapshot      core.Snapshot
	Models        []models.ModelInfo
	UserInfo      *core.UserInfo
	ActiveAccount string
	AuthConfig    *config.AuthConfig
	LogsBlinkOn   bool
	Width         int
	Height        int
	StatusMsg     string
	StatusView    ViewState
}

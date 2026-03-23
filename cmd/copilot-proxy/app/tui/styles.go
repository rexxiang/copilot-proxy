package tui

import "github.com/charmbracelet/lipgloss"

// Styles.
var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(lipgloss.Color("240"))

	SelectedTabStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("229")).
				Background(lipgloss.Color("57"))

	TabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))

	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))

	DimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	StrikeStyle = lipgloss.NewStyle().
			Strikethrough(true)

	ProgressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("39"))

	PremiumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
)

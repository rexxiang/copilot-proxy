package tui

import (
	"fmt"
	"strings"

	"copilot-proxy/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	accountModalWidth    = 56
	accountModalPaddingY = 1
	accountModalPaddingX = 2
)

type AccountModalAction int

const (
	AccountModalActionNone AccountModalAction = iota
	AccountModalActionClose
	AccountModalActionActivate
	AccountModalActionAdd
	AccountModalActionCancelAdd
)

type accountModalMode int

const (
	accountModalModeList accountModalMode = iota
	accountModalModeAuthorizing
)

const accountModalAddLabel = "Add Account"

type AccountModal struct {
	open            bool
	mode            accountModalMode
	accounts        []string
	active          string
	selected        int
	verificationURI string
	userCode        string
	errorMsg        string
}

var accountModalStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("39")).
	Padding(accountModalPaddingY, accountModalPaddingX).
	Width(accountModalWidth)

func NewAccountModal() *AccountModal {
	return &AccountModal{
		mode: accountModalModeList,
	}
}

func (m *AccountModal) Open(auth *config.AuthConfig) error {
	accounts := make([]string, 0)
	active := ""
	if auth != nil {
		accounts = make([]string, 0, len(auth.Accounts))
		for _, account := range auth.Accounts {
			if account.User == "" {
				continue
			}
			accounts = append(accounts, account.User)
		}
		active = auth.Default
	}

	m.accounts = accounts
	m.active = active
	m.selected = 0
	for i := range m.accounts {
		if m.accounts[i] == m.active {
			m.selected = i
			break
		}
	}
	m.mode = accountModalModeList
	m.verificationURI = ""
	m.userCode = ""
	m.errorMsg = ""
	m.open = true
	return nil
}

func (m *AccountModal) IsOpen() bool {
	return m != nil && m.open
}

func (m *AccountModal) Close() {
	if m == nil {
		return
	}
	m.open = false
	m.mode = accountModalModeList
	m.verificationURI = ""
	m.userCode = ""
	m.errorMsg = ""
}

func (m *AccountModal) SelectedUser() string {
	if m == nil || len(m.accounts) == 0 {
		return ""
	}
	if m.selected < 0 || m.selected >= len(m.accounts) {
		return ""
	}
	return m.accounts[m.selected]
}

func (m *AccountModal) BeginAddAuth(verificationURI, userCode string) {
	if m == nil {
		return
	}
	m.mode = accountModalModeAuthorizing
	m.verificationURI = verificationURI
	m.userCode = userCode
	m.errorMsg = ""
}

func (m *AccountModal) EndAddAuthToList() {
	if m == nil {
		return
	}
	m.mode = accountModalModeList
	m.verificationURI = ""
	m.userCode = ""
	if m.selected < 0 {
		m.selected = 0
	}
	maxSelected := len(m.accounts)
	if m.selected > maxSelected {
		m.selected = maxSelected
	}
}

func (m *AccountModal) SetError(message string) {
	if m == nil {
		return
	}
	m.errorMsg = message
}

func (m *AccountModal) HandleKey(msg tea.KeyMsg) AccountModalAction {
	if m == nil || !m.open {
		return AccountModalActionNone
	}
	if m.mode == accountModalModeAuthorizing {
		if msg.String() == "esc" {
			return AccountModalActionCancelAdd
		}
		return AccountModalActionNone
	}

	switch msg.String() {
	case "esc":
		return AccountModalActionClose
	case "enter":
		if m.selected == len(m.accounts) {
			return AccountModalActionAdd
		}
		return AccountModalActionActivate
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return AccountModalActionNone
	case "down", "j":
		if m.selected < len(m.accounts) {
			m.selected++
		}
		return AccountModalActionNone
	default:
		return AccountModalActionNone
	}
}

func (m *AccountModal) View() string {
	if m == nil || !m.open {
		return ""
	}
	if m.mode == accountModalModeAuthorizing {
		return accountModalStyle.Render(m.authorizingView())
	}

	return accountModalStyle.Render(m.listView())
}

func (m *AccountModal) listView() string {
	var sb strings.Builder
	sb.WriteString(TableHeaderStyle.Render("Accounts"))
	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Enter=activate/add  Esc=close  ↑↓=select"))
	sb.WriteString("\n\n")
	for i := range m.accounts {
		user := m.accounts[i]
		line := user
		if user == m.active {
			line += " (active)"
		}
		prefix := "  "
		if i == m.selected {
			prefix = "> "
			sb.WriteString(SelectedTabStyle.Render(prefix + line))
			sb.WriteString("\n")
			continue
		}
		sb.WriteString(prefix + line)
		sb.WriteString("\n")
	}
	addPrefix := "  "
	if m.selected == len(m.accounts) {
		addPrefix = "> "
		sb.WriteString(SelectedTabStyle.Render(addPrefix + accountModalAddLabel))
		sb.WriteString("\n")
	} else {
		sb.WriteString(addPrefix + accountModalAddLabel)
		sb.WriteString("\n")
	}
	if m.errorMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(ErrorStyle.Render(fmt.Sprintf("Account: %s", m.errorMsg)))
	}
	return sb.String()
}

func (m *AccountModal) authorizingView() string {
	var sb strings.Builder
	sb.WriteString(TableHeaderStyle.Render("Add Account"))
	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Esc=cancel"))
	sb.WriteString("\n\n")
	sb.WriteString("Open URL:\n")
	sb.WriteString(m.verificationURI)
	sb.WriteString("\n\n")
	sb.WriteString("Enter code:\n")
	sb.WriteString(SuccessStyle.Render(m.userCode))
	sb.WriteString("\n")
	sb.WriteString(DimStyle.Render("Waiting for authorization..."))
	if m.errorMsg != "" {
		sb.WriteString("\n\n")
		sb.WriteString(ErrorStyle.Render(fmt.Sprintf("Account: %s", m.errorMsg)))
	}
	return sb.String()
}

func (m *AccountModal) Overlay(base string, width, height int) string {
	if m == nil || !m.open {
		return base
	}
	modalView := m.View()
	if width <= 0 || height <= 0 {
		return modalView
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalView)
}

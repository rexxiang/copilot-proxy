package app

import (
	"errors"
	"fmt"
	"os"

	"copilot-proxy/internal/core/account"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type authItem struct {
	dto account.AccountDTO
}

func (a authItem) Title() string {
	if a.dto.IsDefault {
		return a.dto.User + " (default)"
	}
	return a.dto.User
}

func (a authItem) Description() string { return "" }
func (a authItem) FilterValue() string { return a.dto.User }

type accountManager interface {
	List() []account.AccountDTO
	SwitchDefault(user string) error
	Remove(user string) error
}

type authListModel struct {
	list     list.Model
	help     help.Model
	keys     authKeyMap
	manager  accountManager
	err      error
	done     bool
	selected int
}

type authKeyMap struct {
	setDefault key.Binding
	remove     key.Binding
	quit       key.Binding
}

func newAuthKeyMap() authKeyMap {
	return authKeyMap{
		setDefault: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "set default")),
		remove:     key.NewBinding(key.WithKeys("delete", "backspace"), key.WithHelp("del", "remove")),
		quit:       key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q/esc", "quit")),
	}
}

func newAuthListModel(manager accountManager) authListModel {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Copilot Accounts"
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	model := authListModel{
		list:     l,
		help:     help.New(),
		keys:     newAuthKeyMap(),
		manager:  manager,
		err:      nil,
		done:     false,
		selected: 0,
	}
	model.refreshItems()
	return model
}

func (m *authListModel) Init() tea.Cmd { return nil }

func (m *authListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.setDefault):
			if item, ok := m.list.SelectedItem().(authItem); ok {
				if m.manager == nil {
					m.err = errors.New("account manager unavailable")
					return m, tea.Quit
				}
				if err := m.manager.SwitchDefault(item.dto.User); err != nil {
					m.err = err
					return m, tea.Quit
				}
				m.refreshItems()
				m.done = true
				return m, tea.Quit
			}
		case key.Matches(msg, m.keys.remove):
			if item, ok := m.list.SelectedItem().(authItem); ok {
				if m.manager == nil {
					m.err = errors.New("account manager unavailable")
					return m, tea.Quit
				}
				if err := m.manager.Remove(item.dto.User); err != nil {
					m.err = err
					return m, tea.Quit
				}
				m.refreshItems()
				m.done = true
				return m, tea.Quit
			}
		}
	case tea.WindowSizeMsg:
		const listBorderRows = 2
		m.list.SetSize(msg.Width, msg.Height-listBorderRows)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *authListModel) View() string {
	helpText := m.help.View(&m.keys)
	return fmt.Sprintf("%s\n%s", m.list.View(), helpText)
}

func (m *authListModel) refreshItems() {
	if m.manager == nil {
		m.list.SetItems([]list.Item{})
		return
	}
	accounts := m.manager.List()
	items := make([]list.Item, 0, len(accounts))
	defaultIdx := 0
	for i, acct := range accounts {
		if acct.IsDefault {
			defaultIdx = i
		}
		items = append(items, authItem{dto: acct})
	}
	m.list.SetItems(items)
	if len(items) > 0 && defaultIdx < len(items) {
		m.list.Select(defaultIdx)
		m.selected = defaultIdx
	} else {
		m.selected = 0
	}
}

func (k *authKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.setDefault, k.remove, k.quit}
}

func (k *authKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

func runAuthListTUI(manager accountManager) error {
	if manager == nil {
		return errors.New("account manager unavailable")
	}
	accounts := manager.List()
	if len(accounts) == 0 {
		if _, err := fmt.Fprintln(os.Stdout, "No accounts configured"); err != nil {
			return fmt.Errorf("print empty accounts: %w", err)
		}
		return nil
	}

	if !isTTY(os.Stdout.Fd()) {
		return printAccounts(accounts)
	}

	model := newAuthListModel(manager)
	program := tea.NewProgram(&model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return fmt.Errorf("run auth list TUI: %w", err)
	}
	finalModel, ok := result.(*authListModel)
	if ok && finalModel != nil && finalModel.err != nil {
		return finalModel.err
	}
	return nil
}

func printAccounts(accounts []account.AccountDTO) error {
	for _, account := range accounts {
		if account.User == "" {
			continue
		}
		label := account.User
		if account.IsDefault {
			label += " (default)"
		}
		if _, err := fmt.Fprintln(os.Stdout, label); err != nil {
			return fmt.Errorf("print account label: %w", err)
		}
	}
	return nil
}

package cli

import (
	"errors"
	"fmt"
	"os"

	"copilot-proxy/internal/config"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

var errAccountNotFound = errors.New("account not found")

// AuthActions provides callbacks for TUI auth operations.
type AuthActions struct {
	SetDefault func(user string) error
	Remove     func(user string) error
}

// defaultAuthActions creates actions that operate on config directly.
func defaultAuthActions(auth *config.AuthConfig) AuthActions {
	return AuthActions{
		SetDefault: func(user string) error {
			if err := auth.SetDefault(user); err != nil {
				return fmt.Errorf("set default account: %w", err)
			}
			if err := config.SaveAuth(*auth); err != nil {
				return fmt.Errorf("save auth config: %w", err)
			}
			return nil
		},
		Remove: func(user string) error {
			if !auth.RemoveAccount(user) {
				return errAccountNotFound
			}
			if err := config.SaveAuth(*auth); err != nil {
				return fmt.Errorf("save auth config: %w", err)
			}
			return nil
		},
	}
}

type authItem struct {
	user        string
	defaultUser string
}

func (a authItem) Title() string {
	if a.user == a.defaultUser {
		return a.user + " (default)"
	}
	return a.user
}

func (a authItem) Description() string { return "" }
func (a authItem) FilterValue() string { return a.user }

type authListModel struct {
	list    list.Model
	help    help.Model
	keys    authKeyMap
	auth    *config.AuthConfig
	actions AuthActions
	err     error
	done    bool
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

func newAuthListModel(auth *config.AuthConfig, actions AuthActions) authListModel {
	items := make([]list.Item, 0, len(auth.Accounts))
	for _, account := range auth.Accounts {
		items = append(items, authItem{user: account.User, defaultUser: auth.Default})
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Copilot Accounts"
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	return authListModel{
		list:    l,
		help:    help.New(),
		keys:    newAuthKeyMap(),
		auth:    auth,
		actions: actions,
		err:     nil,
		done:    false,
	}
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
				if err := m.actions.SetDefault(item.user); err != nil {
					m.err = err
					return m, tea.Quit
				}
				m.auth.Default = item.user
				m.refreshItems()
				m.done = true
				return m, tea.Quit
			}
		case key.Matches(msg, m.keys.remove):
			if item, ok := m.list.SelectedItem().(authItem); ok {
				if err := m.actions.Remove(item.user); err != nil {
					m.err = err
					return m, tea.Quit
				}
				m.auth.RemoveAccount(item.user)
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
	items := make([]list.Item, 0, len(m.auth.Accounts))
	for _, account := range m.auth.Accounts {
		items = append(items, authItem{user: account.User, defaultUser: m.auth.Default})
	}
	m.list.SetItems(items)
}

func (k *authKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.setDefault, k.remove, k.quit}
}

func (k *authKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

func runAuthListTUI() error {
	cfg, err := config.LoadAuth()
	if err != nil {
		return fmt.Errorf("load auth config: %w", err)
	}
	if len(cfg.Accounts) == 0 {
		_, err := fmt.Fprintln(os.Stdout, "No accounts configured")
		if err != nil {
			return fmt.Errorf("print empty accounts: %w", err)
		}
		return nil
	}

	if !isTTY(os.Stdout.Fd()) {
		return printAccounts(cfg)
	}

	actions := defaultAuthActions(&cfg)
	model := newAuthListModel(&cfg, actions)
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

func printAccounts(cfg config.AuthConfig) error {
	for _, account := range cfg.Accounts {
		label := account.User
		if account.User == cfg.Default {
			label += " (default)"
		}
		if _, err := fmt.Fprintln(os.Stdout, label); err != nil {
			return fmt.Errorf("print account label: %w", err)
		}
	}
	return nil
}

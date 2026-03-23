package app

import (
	"context"
	"errors"
	"fmt"

	"copilot-proxy/cmd/copilot-proxy/app/tui"
	"copilot-proxy/internal/runtime/config"
	"copilot-proxy/internal/runtime/identity/account"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *MonitorModel) handleOpenAccountModal() (tea.Model, tea.Cmd) {
	if m.state != tui.ViewStats {
		return m, nil
	}
	if m.accountModal == nil {
		m.accountModal = tui.NewAccountModal()
	}
	if err := m.openAccountModal(); err != nil {
		m.setStatus(tui.ViewStats, fmt.Sprintf("Account: %v", err))
	}
	return m, nil
}

func (m *MonitorModel) handleAccountModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := m.accountModal.HandleKey(msg)
	switch action {
	case tui.AccountModalActionNone:
		return m, nil
	case tui.AccountModalActionClose:
		m.cancelAccountAuth(true)
		m.accountModal.Close()
		return m, nil
	case tui.AccountModalActionActivate:
		user := m.accountModal.SelectedUser()
		if user == "" {
			m.accountModal.SetError("no account selected")
			return m, nil
		}
		if err := m.activateSelectedAccount(user); err != nil {
			m.accountModal.SetError(err.Error())
			return m, nil
		}
		if m.sharedState != nil {
			m.sharedState.ActiveAccount = user
		}
		m.accountModal.Close()
		m.userInfo = nil
		m.sharedState.UserInfo = nil
		m.loadedUserInfo = false
		return m, m.beginUserInfoRefreshDeferred()
	case tui.AccountModalActionAdd:
		return m.startAddAccountFlow()
	case tui.AccountModalActionCancelAdd:
		m.cancelAccountAuth(true)
		m.accountModal.EndAddAuthToList()
		m.setStatus(tui.ViewStats, "Account add canceled")
		return m, nil
	default:
		return m, nil
	}
}

func (m *MonitorModel) activateSelectedAccount(user string) error {
	if m.accountService == nil {
		return errNoAuthConfigured
	}
	if err := m.accountService.SwitchDefault(user); err != nil {
		return err
	}
	if m.sharedState != nil {
		m.sharedState.ActiveAccount = user
	}
	return nil
}

func (m *MonitorModel) startAddAccountFlow() (tea.Model, tea.Cmd) {
	m.cancelAccountAuth(true)
	if m.accountService == nil {
		if m.accountModal != nil {
			m.accountModal.SetError(errNoAuthConfigured.Error())
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.accountAuthCtx = ctx
	m.accountAuthDone = cancel
	m.setStatus(tui.ViewStats, "Requesting device code...")
	return m, m.beginAccountLoginCmd(ctx)
}

func (m *MonitorModel) cancelAccountAuth(invalidateSeq bool) {
	if m.accountAuthDone != nil {
		m.accountAuthDone()
		m.accountAuthDone = nil
	}
	if m.accountService != nil {
		m.accountService.CancelLogin(0)
	}
	m.accountAuthCtx = nil
	if invalidateSeq {
		m.accountAuthSeq = 0
	}
}

func (m *MonitorModel) beginAccountLoginCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		if m.accountService == nil {
			return accountLoginChallengeMsg{
				err: errNoAuthConfigured,
			}
		}
		challenge, err := m.accountService.BeginLogin(ctx)
		return accountLoginChallengeMsg{
			seq:       challenge.Seq,
			challenge: challenge,
			err:       err,
		}
	}
}

func (m *MonitorModel) pollAccountLoginCmd(ctx context.Context, seq int64) tea.Cmd {
	return func() tea.Msg {
		if m.accountService == nil {
			return accountLoginResultMsg{
				seq: seq,
				err: errNoAuthConfigured,
			}
		}
		result, err := m.accountService.PollLogin(ctx, seq)
		return accountLoginResultMsg{
			seq:    seq,
			result: result,
			err:    err,
		}
	}
}

func (m *MonitorModel) handleAccountLoginChallenge(msg accountLoginChallengeMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.cancelAccountAuth(false)
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(fmt.Sprintf("request device code: %v", msg.err))
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}
	m.accountAuthSeq = msg.seq
	if m.accountModal != nil {
		m.accountModal.BeginAddAuth(msg.challenge.VerificationURI, msg.challenge.UserCode)
	}
	m.setStatus(tui.ViewStats, "Waiting for device authorization...")
	ctx := m.accountAuthCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return m, m.pollAccountLoginCmd(ctx, msg.seq)
}

func (m *MonitorModel) handleAccountLoginResult(msg accountLoginResultMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.accountAuthSeq {
		return m, nil
	}
	m.cancelAccountAuth(false)
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			if m.accountModal != nil {
				m.accountModal.EndAddAuthToList()
			}
			return m, nil
		}
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(fmt.Sprintf("poll login: %v", msg.err))
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}
	if msg.result.Login == "" || msg.result.Token == "" {
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError("poll login: invalid credentials")
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}

	account := config.Account{
		User:    msg.result.Login,
		GhToken: msg.result.Token,
		AppID:   "",
	}

	hadAccounts := false
	if m.accountService != nil && len(m.accountService.List()) > 0 {
		hadAccounts = true
	}

	if err := m.addAuthorizedAccount(account); err != nil {
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(err.Error())
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}

	m.refreshActiveAccount()

	if m.accountModal != nil {
		m.accountModal.EndAddAuthToList()
		if err := m.openAccountModal(); err != nil {
			m.setStatus(tui.ViewStats, fmt.Sprintf("Account: %v", err))
			return m, nil
		}
	}

	m.setStatus(tui.ViewStats, fmt.Sprintf("Account added: %s", account.User))
	if !hadAccounts {
		m.userInfo = nil
		m.sharedState.UserInfo = nil
		m.loadedUserInfo = false
		return m, m.beginUserInfoRefreshDeferred()
	}
	return m, nil
}

func (m *MonitorModel) openAccountModal() error {
	if m.accountModal == nil {
		return nil
	}
	accounts, active := m.accountDTOsForModal()
	return m.accountModal.Open(accounts, active)
}

func (m *MonitorModel) addAuthorizedAccount(account config.Account) error {
	if m.accountService == nil {
		return errNoAuthConfigured
	}
	return m.accountService.Add(account)
}

func (m *MonitorModel) accountDTOsForModal() ([]account.AccountDTO, string) {
	if m.accountService == nil {
		return nil, ""
	}
	var active string
	if acct, _, err := m.accountService.Current(); err == nil {
		active = acct.User
	}
	return m.accountService.List(), active
}

func (m *MonitorModel) refreshActiveAccount() {
	if m.accountService == nil || m.sharedState == nil {
		return
	}
	if acct, _, err := m.accountService.Current(); err == nil {
		m.sharedState.ActiveAccount = acct.User
	}
}

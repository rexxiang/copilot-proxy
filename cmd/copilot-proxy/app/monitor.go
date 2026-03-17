package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"copilot-proxy/cmd/copilot-proxy/app/debounce"
	appsettings "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/cmd/copilot-proxy/app/tui"
	"copilot-proxy/internal/runtime/config"
	"copilot-proxy/internal/runtime/identity/account"
	models "copilot-proxy/internal/runtime/model"
	"copilot-proxy/internal/runtime/observability"
	"copilot-proxy/internal/runtime/stats"
	core "copilot-proxy/internal/runtime/types"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errNoAuthConfigured = errors.New("no auth configured")

var errSettingsApplyResultNil = errors.New("settings apply result is nil")

// MonitorDeps contains dependencies for creating a MonitorModel.
type MonitorDeps struct {
	Collector      statsCollector
	StatsService   statsService
	ModelService   modelService
	AccountService accountService
	Models         []models.ModelInfo
	UserInfo       *core.UserInfo
	AuthConfig     *config.AuthConfig
	HTTPClient     *http.Client
	LoadSettings   func() (appsettings.Settings, error)
	ApplySettings  func(appsettings.Settings) (appsettings.Settings, error)
}

// monitorKeyMap defines keybindings for the monitor TUI.
type monitorKeyMap struct {
	left      key.Binding
	right     key.Binding
	stats     key.Binding
	models    key.Binding
	logs      key.Binding
	refresh   key.Binding
	up        key.Binding
	down      key.Binding
	pageUp    key.Binding
	pageDown  key.Binding
	expand    key.Binding
	settings  key.Binding
	accounts  key.Binding
	clearLogs key.Binding
	quit      key.Binding
}

const monitorRequestTimeout = 30 * time.Second

type statsCollector interface {
	Snapshot() core.Snapshot
	Reset()
}

type statsService interface {
	MonitorSnapshot() core.Snapshot
	Reset()
}

type modelService interface {
	List() []models.ModelInfo
	Refresh(ctx context.Context) ([]models.ModelInfo, error)
}

type accountService interface {
	List() []account.AccountDTO
	Current() (config.Account, bool, error)
	SwitchDefault(user string) error
	Add(account config.Account) error
	Remove(user string) error
	BeginLogin(ctx context.Context) (account.LoginChallenge, error)
	PollLogin(ctx context.Context, seq int64) (account.LoginResult, error)
	CancelLogin(seq int64)
	PremiumInfo(ctx context.Context, force bool) (core.UserInfo, error)
	InvalidatePremium(user string)
}

type observabilityProvider interface {
	Observability() *observability.Observability
}

func newMonitorKeyMap() monitorKeyMap {
	return monitorKeyMap{
		left:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev")),
		right:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next")),
		stats:     key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "stats")),
		models:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "models")),
		logs:      key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "logs")),
		refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", "up")),
		down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", "down")),
		pageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		pageDown:  key.NewBinding(key.WithKeys("pgdn"), key.WithHelp("PgDn", "page down")),
		expand:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "expand")),
		settings:  key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^S", "settings")),
		accounts:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "accounts")),
		clearLogs: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear")),
		quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^C", "quit")),
	}
}

func (k *monitorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.left, k.right, k.refresh, k.accounts, k.settings, k.quit}
}

func (k *monitorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.left, k.right, k.stats, k.models, k.logs},
		{k.refresh, k.accounts, k.settings, k.quit},
	}
}

// Messages for async operations.
type tickMsg time.Time
type modelsLoadedMsg struct {
	models []models.ModelInfo
	err    error
}
type userInfoLoadedMsg struct {
	info *core.UserInfo
	err  error
}

type accountLoginChallengeMsg struct {
	seq       int64
	challenge account.LoginChallenge
	err       error
}

type accountLoginResultMsg struct {
	seq    int64
	result account.LoginResult
	err    error
}

type settingsAppliedMsg struct {
	settings *appsettings.Settings
	err      error
}

// MonitorModel is the main bubbletea model for the monitor TUI.
type MonitorModel struct {
	state                        tui.ViewState
	statsService                 statsService
	modelService                 modelService
	accountService               accountService
	width                        int
	height                       int
	help                         help.Model
	keys                         monitorKeyMap
	serverAddr                   string
	quitting                     bool
	loading                      bool
	lastRefresh                  time.Time
	loadSettings                 func() (appsettings.Settings, error)
	applySettings                func(appsettings.Settings) (appsettings.Settings, error)
	currentSettings              appsettings.Settings
	statusMsg                    string
	statusView                   tui.ViewState
	snapshot                     core.Snapshot
	loadedUserInfo               bool
	loadedModels                 bool
	userInfoQueue                debounce.Debouncer
	premiumDetector              agentPremiumRefreshDetector
	userInfoRefreshAfterInFlight bool
	userInfoForceRefresh         bool

	// View components
	statsView       *tui.StatsView
	modelsView      *tui.ModelsView
	logsView        *tui.LogsView
	configModal     *tui.ConfigModal
	accountModal    *tui.AccountModal
	sharedState     *tui.SharedState
	userInfo        *core.UserInfo
	accountAuthSeq  int64
	accountAuthDone context.CancelFunc
	accountAuthCtx  context.Context
}

// NewMonitorModel creates a new monitor TUI model.
func NewMonitorModel(deps *MonitorDeps, serverAddr string) MonitorModel {
	if deps == nil {
		deps = &MonitorDeps{}
	}

	statsSvc := resolveStatsService(deps)
	modelSvc := resolveModelService(deps, serverAddr)
	accountSvc := resolveAccountService(deps)
	loadSettings := deps.LoadSettings
	currentSettings := appsettings.DefaultSettings()
	if loadSettings == nil {
		settingsMu := sync.Mutex{}
		loadSettings = func() (appsettings.Settings, error) {
			settingsMu.Lock()
			defer settingsMu.Unlock()
			return currentSettings, nil
		}
		applySettings := deps.ApplySettings
		if applySettings == nil {
			applySettings = func(settings appsettings.Settings) (appsettings.Settings, error) {
				settingsMu.Lock()
				defer settingsMu.Unlock()
				currentSettings = settings
				return currentSettings, nil
			}
		}
		deps.ApplySettings = applySettings
	}
	applySettings := deps.ApplySettings
	currentSettings, err := loadSettings()
	if err != nil {
		currentSettings = appsettings.DefaultSettings()
	}
	if applySettings == nil {
		applySettings = func(settings appsettings.Settings) (appsettings.Settings, error) {
			return settings, nil
		}
	}

	statsSvc.MonitorSnapshot()
	modelsList := modelSvc.List()
	if deps != nil && len(deps.Models) > 0 {
		modelsList = deps.Models
	}
	sharedState := &tui.SharedState{
		Snapshot:      emptySnapshot(),
		Models:        modelsList,
		UserInfo:      deps.UserInfo,
		ActiveAccount: "",
		LogsBlinkOn:   true,
		StatusView:    tui.ViewStats,
	}
	if accountSvc != nil {
		if acct, _, err := accountSvc.Current(); err == nil {
			sharedState.ActiveAccount = acct.User
		}
	}

	model := MonitorModel{
		state:           tui.ViewStats,
		statsService:    statsSvc,
		modelService:    modelSvc,
		accountService:  accountSvc,
		help:            help.New(),
		keys:            newMonitorKeyMap(),
		serverAddr:      serverAddr,
		loadSettings:    loadSettings,
		applySettings:   applySettings,
		currentSettings: currentSettings,
		statusView:      tui.ViewStats,
		snapshot:        emptySnapshot(),
		sharedState:     sharedState,
		userInfoQueue:   debounce.New(userInfoRefreshDebounceDelay, debounce.ModeLeading),
		premiumDetector: newAgentPremiumRefreshDetector(),
		statsView:       tui.NewStatsView(),
		modelsView:      tui.NewModelsView(),
		logsView:        tui.NewLogsView(),
		configModal:     tui.NewConfigModal(),
		accountModal:    tui.NewAccountModal(),
	}

	model.loadedModels = len(sharedState.Models) > 0
	model.loadedUserInfo = sharedState.UserInfo != nil

	model.statsView.SetState(sharedState)
	model.modelsView.SetState(sharedState)
	model.modelsView.SetModels(sharedState.Models)
	model.logsView.SetState(sharedState)

	return model
}

func resolveStatsService(deps *MonitorDeps) statsService {
	if deps != nil && deps.StatsService != nil {
		return deps.StatsService
	}
	if deps != nil && deps.Collector != nil {
		if provider, ok := deps.Collector.(observabilityProvider); ok {
			return stats.NewService(provider.Observability())
		}
	}
	return stats.NewService(nil)
}

func resolveModelService(deps *MonitorDeps, serverAddr string) modelService {
	if deps != nil && deps.ModelService != nil {
		return deps.ModelService
	}
	client := newMonitorHTTPClient()
	if deps != nil && deps.HTTPClient != nil {
		client = deps.HTTPClient
	}
	proxyURL := serverAddr
	if !strings.HasPrefix(proxyURL, "http") {
		proxyURL = "http://" + proxyURL
	}
	return models.NewService(models.NewManager(), nil, client, proxyURL)
}

func resolveAccountService(deps *MonitorDeps) accountService {
	if deps != nil && deps.AccountService != nil {
		return deps.AccountService
	}
	var authCfg config.AuthConfig
	if deps != nil && deps.AuthConfig != nil {
		authCfg = *deps.AuthConfig
	}
	var mu sync.Mutex
	loadAuth := func() (config.AuthConfig, error) {
		mu.Lock()
		defer mu.Unlock()
		cloned := authCfg
		cloned.Accounts = append([]config.Account(nil), authCfg.Accounts...)
		return cloned, nil
	}
	saveAuth := func(next config.AuthConfig) error {
		mu.Lock()
		defer mu.Unlock()
		authCfg = next
		authCfg.Accounts = append([]config.Account(nil), next.Accounts...)
		return nil
	}
	return NewAccountManager(AccountManagerDeps{
		LoadAuth: loadAuth,
		SaveAuth: saveAuth,
	})
}

// Init initializes the model.
func (m *MonitorModel) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if cmd := m.enqueueUserInfoRefresh(userInfoRefreshSourceStartup); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if !m.loadedModels {
		cmds = append(cmds, m.loadModelsCmd())
	}

	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *MonitorModel) loadModelsCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), monitorRequestTimeout)
		defer cancel()
		items, err := m.modelService.Refresh(ctx)
		return modelsLoadedMsg{models: items, err: err}
	}
}

func (m *MonitorModel) loadUserInfoCmd(force bool) tea.Cmd {
	return func() tea.Msg {
		if m.accountService == nil {
			return userInfoLoadedMsg{info: nil, err: errNoAuthConfigured}
		}

		ctx, cancel := context.WithTimeout(context.Background(), monitorRequestTimeout)
		defer cancel()

		info, err := m.accountService.PremiumInfo(ctx, force)
		return userInfoLoadedMsg{info: &info, err: err}
	}
}

// Update handles messages and updates the model.
func (m *MonitorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
	case tickMsg:
		return m.handleTick()
	case modelsLoadedMsg:
		m.handleModelsLoaded(msg)
	case userInfoLoadedMsg:
		return m.handleUserInfoLoaded(msg)
	case userInfoRefreshDueMsg:
		return m.handleUserInfoRefreshDue(msg)
	case accountLoginChallengeMsg:
		return m.handleAccountLoginChallenge(msg)
	case accountLoginResultMsg:
		return m.handleAccountLoginResult(msg)
	case settingsAppliedMsg:
		m.handleSettingsApplied(&msg)
	}
	m.sharedState.StatusMsg = m.statusMsg
	m.sharedState.StatusView = m.statusView
	return m, nil
}

func emptySnapshot() core.Snapshot {
	return core.Snapshot{}
}

func (m *MonitorModel) setStatus(view tui.ViewState, message string) {
	m.statusMsg = message
	m.statusView = view
	if m.sharedState != nil {
		m.sharedState.StatusMsg = message
		m.sharedState.StatusView = view
	}
}

func newMonitorHTTPClient() *http.Client {
	return &http.Client{Timeout: monitorRequestTimeout}
}

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

func (m *MonitorModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.quit) {
		return m.handleQuit()
	}
	if m.configModal != nil && m.configModal.IsOpen() {
		return m.handleConfigModalKey(msg)
	}
	if m.accountModal != nil && m.accountModal.IsOpen() {
		return m.handleAccountModalKey(msg)
	}

	if handled, model, cmd := m.handleGlobalKey(msg); handled {
		return model, cmd
	}
	return m.handleCurrentViewKey(msg)
}

func (m *MonitorModel) handleGlobalKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.left):
		model, cmd := m.handlePrevView()
		return true, model, cmd
	case key.Matches(msg, m.keys.right):
		model, cmd := m.handleNextView()
		return true, model, cmd
	case key.Matches(msg, m.keys.stats):
		model, cmd := m.handleDirectView(tui.ViewStats)
		return true, model, cmd
	case key.Matches(msg, m.keys.models):
		model, cmd := m.handleDirectView(tui.ViewModels)
		return true, model, cmd
	case key.Matches(msg, m.keys.logs):
		model, cmd := m.handleDirectView(tui.ViewLogs)
		return true, model, cmd
	case key.Matches(msg, m.keys.refresh):
		model, cmd := m.handleRefresh()
		return true, model, cmd
	case key.Matches(msg, m.keys.clearLogs):
		model, cmd := m.handleClearLogsKey(msg)
		return true, model, cmd
	case key.Matches(msg, m.keys.settings):
		model := m.handleOpenSettingsModal()
		return true, model, nil
	case key.Matches(msg, m.keys.accounts):
		model, cmd := m.handleOpenAccountModal()
		return true, model, cmd
	case key.Matches(msg, m.keys.expand):
		// Expand key now does nothing (detail view removed)
		return true, m, nil
	default:
		return false, nil, nil
	}
}

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

func (m *MonitorModel) handleQuit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

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

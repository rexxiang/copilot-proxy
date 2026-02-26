package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/cli/tui"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/models"
	"copilot-proxy/internal/monitor"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errNoAuthConfigured = errors.New("no auth configured")

var errSettingsApplyResultNil = errors.New("settings apply result is nil")

// MonitorDeps contains dependencies for creating a MonitorModel.
type MonitorDeps struct {
	Collector       monitor.Collector
	DebugLogger     *monitor.DebugLogger
	Models          []monitor.ModelInfo
	UserInfo        *monitor.UserInfo
	AuthConfig      *config.AuthConfig
	ActivateAccount func(user string) error
	AddAccount      func(account config.Account) error
	HTTPClient      *http.Client
	ProxyInvoker    models.RequestDoer
	LogDir          string // Directory for debug log files
	LoadSettings    func() (config.Settings, error)
	ApplySettings   func(config.Settings) (config.Settings, error)
}

// monitorKeyMap defines keybindings for the monitor TUI.
type monitorKeyMap struct {
	left      key.Binding
	right     key.Binding
	stats     key.Binding
	models    key.Binding
	activity  key.Binding
	logs      key.Binding
	prevMode  key.Binding
	nextMode  key.Binding
	refresh   key.Binding
	up        key.Binding
	down      key.Binding
	pageUp    key.Binding
	pageDown  key.Binding
	debug     key.Binding
	expand    key.Binding
	settings  key.Binding
	accounts  key.Binding
	clearLogs key.Binding
	quit      key.Binding
}

const monitorRequestTimeout = 30 * time.Second

func newMonitorKeyMap() monitorKeyMap {
	return monitorKeyMap{
		left:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev")),
		right:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next")),
		stats:     key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "stats")),
		models:    key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "models")),
		activity:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "activity")),
		logs:      key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "logs")),
		prevMode:  key.NewBinding(key.WithKeys(",", "，", "<", "《"), key.WithHelp(",/<", "prev mode")),
		nextMode:  key.NewBinding(key.WithKeys(".", "。", ">", "》"), key.WithHelp("./>", "next mode")),
		refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑", "up")),
		down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓", "down")),
		pageUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("PgUp", "page up")),
		pageDown:  key.NewBinding(key.WithKeys("pgdn"), key.WithHelp("PgDn", "page down")),
		debug:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "debug")),
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
		{k.left, k.right, k.stats, k.models, k.activity, k.logs},
		{k.refresh, k.accounts, k.settings, k.quit},
	}
}

// Messages for async operations.
type tickMsg time.Time
type modelsLoadedMsg struct {
	models []monitor.ModelInfo
	err    error
}
type userInfoLoadedMsg struct {
	info *monitor.UserInfo
	err  error
}

type accountDeviceCodeMsg struct {
	seq    int
	device auth.DeviceCodeResponse
	err    error
}

type accountTokenMsg struct {
	seq   int
	token string
	err   error
}

type accountUserMsg struct {
	seq   int
	login string
	token string
	err   error
}

type settingsAppliedMsg struct {
	settings *config.Settings
	err      error
}

// MonitorModel is the main bubbletea model for the monitor TUI.
type MonitorModel struct {
	state           tui.ViewState
	collector       monitor.Collector
	debugLogger     *monitor.DebugLogger
	models          []monitor.ModelInfo
	userInfo        *monitor.UserInfo
	snapshot        monitor.Snapshot
	width           int
	height          int
	help            help.Model
	keys            monitorKeyMap
	serverAddr      string
	quitting        bool
	loading         bool
	lastRefresh     time.Time
	authConfig      *config.AuthConfig
	httpClient      *http.Client
	proxyInvoker    models.RequestDoer
	loadSettings    func() (config.Settings, error)
	applySettings   func(config.Settings) (config.Settings, error)
	currentSettings config.Settings
	statusMsg       string
	statusView      tui.ViewState // Which view the status message belongs to
	loadedUserInfo  bool
	loadedModels    bool

	// View components
	statsView       *tui.StatsView
	activityView    *tui.ActivityView
	modelsView      *tui.ModelsView
	logsView        *tui.LogsView
	configModal     *tui.ConfigModal
	accountModal    *tui.AccountModal
	sharedState     *tui.SharedState
	activateAccount func(user string) error
	addAccount      func(account config.Account) error
	accountAuthSeq  int
	accountAuthDone context.CancelFunc
}

// NewMonitorModel creates a new monitor TUI model.
func NewMonitorModel(deps *MonitorDeps, serverAddr string) MonitorModel {
	if deps == nil {
		deps = &MonitorDeps{}
	}
	loadSettings := deps.LoadSettings
	if loadSettings == nil {
		loadSettings = config.LoadSettings
	}
	applySettings := deps.ApplySettings
	if applySettings == nil {
		applySettings = func(settings config.Settings) (config.Settings, error) {
			if err := config.SaveSettings(&settings); err != nil {
				return config.Settings{}, fmt.Errorf("save settings: %w", err)
			}
			return settings, nil
		}
	}

	sharedState := &tui.SharedState{
		Snapshot:   emptySnapshot(),
		Models:     deps.Models,
		UserInfo:   deps.UserInfo,
		AuthConfig: deps.AuthConfig,
		StatusView: tui.ViewStats,
	}

	model := MonitorModel{
		state:           tui.ViewStats,
		collector:       deps.Collector,
		debugLogger:     deps.DebugLogger,
		models:          deps.Models,
		userInfo:        deps.UserInfo,
		snapshot:        emptySnapshot(),
		help:            help.New(),
		keys:            newMonitorKeyMap(),
		serverAddr:      serverAddr,
		authConfig:      deps.AuthConfig,
		activateAccount: deps.ActivateAccount,
		addAccount:      deps.AddAccount,
		httpClient:      deps.HTTPClient,
		proxyInvoker:    deps.ProxyInvoker,
		loadSettings:    loadSettings,
		applySettings:   applySettings,
		currentSettings: config.DefaultSettings(),
		statusView:      tui.ViewStats,
		sharedState:     sharedState,

		// Initialize view components
		statsView:    tui.NewStatsView(),
		activityView: tui.NewActivityView(),
		modelsView:   tui.NewModelsView(),
		logsView:     tui.NewLogsView(),
		configModal:  tui.NewConfigModal(),
		accountModal: tui.NewAccountModal(),
	}

	if models.DefaultModelsManager().HasModels() {
		model.loadedModels = true
	}

	// Set up view components
	model.statsView.SetState(sharedState)
	model.activityView.SetState(sharedState)
	model.modelsView.SetState(sharedState)
	model.logsView.SetState(sharedState)

	model.modelsView.SetModels(deps.Models)
	model.logsView.SetDebugLogger(deps.DebugLogger)

	return model
}

// Init initializes the model.
func (m *MonitorModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		m.loadUserInfoCmd(),
	}

	// Only load models if global cache is empty
	if !models.DefaultModelsManager().HasModels() {
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
		client := m.httpClient
		if client == nil {
			client = newMonitorHTTPClient()
		}

		ctx, cancel := context.WithTimeout(context.Background(), monitorRequestTimeout)
		defer cancel()

		if m.proxyInvoker != nil {
			items, err := models.FetchViaDoer(
				ctx,
				m.proxyInvoker,
				"http://in-process"+config.ModelsPath,
			)
			if err == nil || !shouldFallbackToProxyURL(err) || m.serverAddr == "" {
				return modelsLoadedMsg{models: items, err: err}
			}
		}

		// Use proxy to fetch models - the proxy handles auth and header injection
		proxyURL := "http://" + m.serverAddr
		items, err := models.FetchViaProxy(ctx, client, proxyURL)
		return modelsLoadedMsg{models: items, err: err}
	}
}

func shouldFallbackToProxyURL(err error) bool {
	if err == nil {
		return false
	}
	if models.IsHTTPStatus(err, http.StatusNotFound) || models.IsHTTPStatus(err, http.StatusMethodNotAllowed) {
		return true
	}
	var statusErr *models.HTTPStatusError
	return !errors.As(err, &statusErr)
}

func (m *MonitorModel) loadUserInfoCmd() tea.Cmd {
	return func() tea.Msg {
		if m.authConfig == nil || len(m.authConfig.Accounts) == 0 {
			return userInfoLoadedMsg{info: nil, err: errNoAuthConfigured}
		}

		account, _, err := m.authConfig.DefaultAccount()
		if err != nil {
			return userInfoLoadedMsg{info: nil, err: err}
		}

		client := m.httpClient
		if client == nil {
			client = newMonitorHTTPClient()
		}

		ctx, cancel := context.WithTimeout(context.Background(), monitorRequestTimeout)
		defer cancel()

		info, err := monitor.FetchUserInfo(ctx, client, config.GitHubAPIURL, account.GhToken)
		return userInfoLoadedMsg{info: info, err: err}
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
		m.handleUserInfoLoaded(msg)
	case accountDeviceCodeMsg:
		return m.handleAccountDeviceCode(msg)
	case accountTokenMsg:
		return m.handleAccountToken(msg)
	case accountUserMsg:
		return m.handleAccountUser(msg)
	case settingsAppliedMsg:
		m.handleSettingsApplied(&msg)
	}
	m.sharedState.StatusMsg = m.statusMsg
	m.sharedState.StatusView = m.statusView
	return m, nil
}

func emptySnapshot() monitor.Snapshot {
	return monitor.Snapshot{}
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
	m.loading = true
	m.setStatus(tui.ViewStats, "Refreshing user info...")
	return m.loadUserInfoCmd()
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
	case key.Matches(msg, m.keys.activity):
		model, cmd := m.handleDirectView(tui.ViewActivity)
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
	if err := m.accountModal.Open(m.authConfig); err != nil {
		m.setStatus(tui.ViewStats, fmt.Sprintf("Account: %v", err))
		return m, nil
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
		if m.authConfig != nil {
			m.authConfig.Default = user
		}
		m.accountModal.Close()
		m.userInfo = nil
		m.sharedState.UserInfo = nil
		m.loadedUserInfo = false
		return m, m.beginUserInfoRefresh()
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
	if m.activateAccount != nil {
		return m.activateAccount(user)
	}
	if m.authConfig == nil {
		return errNoAuthConfigured
	}
	if err := m.authConfig.SetDefault(user); err != nil {
		return err
	}
	if err := config.SaveAuth(*m.authConfig); err != nil {
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func (m *MonitorModel) startAddAccountFlow() (tea.Model, tea.Cmd) {
	m.cancelAccountAuth(false)
	m.accountAuthSeq++
	seq := m.accountAuthSeq

	ctx, cancel := context.WithCancel(context.Background())
	m.accountAuthDone = cancel
	m.setStatus(tui.ViewStats, "Requesting device code...")
	return m, m.requestDeviceCodeCmd(ctx, seq)
}

func (m *MonitorModel) cancelAccountAuth(invalidateSeq bool) {
	if m.accountAuthDone != nil {
		m.accountAuthDone()
		m.accountAuthDone = nil
	}
	if invalidateSeq {
		m.accountAuthSeq++
	}
}

func (m *MonitorModel) requestDeviceCodeCmd(ctx context.Context, seq int) tea.Cmd {
	return func() tea.Msg {
		flow := newDefaultDeviceFlow()
		device, err := flow.RequestCodeWithContext(ctx)
		return accountDeviceCodeMsg{
			seq:    seq,
			device: device,
			err:    err,
		}
	}
}

func (m *MonitorModel) pollAccessTokenCmd(
	ctx context.Context,
	seq int,
	device auth.DeviceCodeResponse,
) tea.Cmd {
	return func() tea.Msg {
		flow := newDefaultDeviceFlow()
		tokenValue, err := flow.PollAccessTokenWithContext(ctx, device)
		return accountTokenMsg{
			seq:   seq,
			token: tokenValue,
			err:   err,
		}
	}
}

func (m *MonitorModel) fetchUserLoginCmd(seq int, tokenValue string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), monitorRequestTimeout)
		defer cancel()
		login, err := auth.FetchUserWithContext(ctx, nil, "", tokenValue)
		return accountUserMsg{
			seq:   seq,
			login: login,
			token: tokenValue,
			err:   err,
		}
	}
}

func (m *MonitorModel) handleAccountDeviceCode(msg accountDeviceCodeMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.accountAuthSeq {
		return m, nil
	}
	if msg.err != nil {
		m.cancelAccountAuth(false)
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(fmt.Sprintf("request device code: %v", msg.err))
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}

	if m.accountModal != nil {
		m.accountModal.BeginAddAuth(msg.device.VerificationURI, msg.device.UserCode)
	}
	m.setStatus(tui.ViewStats, "Waiting for device authorization...")

	m.cancelAccountAuth(false)
	ctx, cancel := context.WithCancel(context.Background())
	m.accountAuthDone = cancel
	return m, m.pollAccessTokenCmd(ctx, msg.seq, msg.device)
}

func (m *MonitorModel) handleAccountToken(msg accountTokenMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.accountAuthSeq {
		return m, nil
	}
	if msg.err != nil {
		m.cancelAccountAuth(false)
		if errors.Is(msg.err, context.Canceled) {
			return m, nil
		}
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(fmt.Sprintf("poll access token: %v", msg.err))
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}
	return m, m.fetchUserLoginCmd(msg.seq, msg.token)
}

func (m *MonitorModel) handleAccountUser(msg accountUserMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.accountAuthSeq {
		return m, nil
	}
	m.cancelAccountAuth(false)
	if msg.err != nil {
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(fmt.Sprintf("fetch user: %v", msg.err))
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}

	account := config.Account{
		User:    msg.login,
		GhToken: msg.token,
		AppID:   "",
	}

	hadAccounts := m.authConfig != nil && len(m.authConfig.Accounts) > 0
	previousDefault := ""
	if m.authConfig != nil {
		previousDefault = m.authConfig.Default
	}

	if err := m.addAuthorizedAccount(account); err != nil {
		if m.accountModal != nil {
			m.accountModal.EndAddAuthToList()
			m.accountModal.SetError(err.Error())
		}
		m.setStatus(tui.ViewStats, "Account add failed")
		return m, nil
	}

	if m.authConfig == nil {
		m.authConfig = &config.AuthConfig{
			Default:  "",
			Accounts: nil,
		}
	}
	m.authConfig.UpsertAccount(account)
	if hadAccounts && previousDefault != "" && previousDefault != account.User {
		_ = m.authConfig.SetDefault(previousDefault)
	}
	m.sharedState.AuthConfig = m.authConfig

	if m.accountModal != nil {
		if err := m.accountModal.Open(m.authConfig); err != nil {
			m.setStatus(tui.ViewStats, fmt.Sprintf("Account: %v", err))
			return m, nil
		}
	}

	m.setStatus(tui.ViewStats, fmt.Sprintf("Account added: %s", account.User))

	if !hadAccounts {
		m.userInfo = nil
		m.sharedState.UserInfo = nil
		m.loadedUserInfo = false
		return m, m.beginUserInfoRefresh()
	}

	return m, nil
}

func (m *MonitorModel) addAuthorizedAccount(account config.Account) error {
	if m.addAccount != nil {
		return m.addAccount(account)
	}

	if m.authConfig == nil {
		m.authConfig = &config.AuthConfig{
			Default:  "",
			Accounts: nil,
		}
	}
	previous := cloneAuthConfig(*m.authConfig)
	hadAccounts := len(m.authConfig.Accounts) > 0
	previousDefault := m.authConfig.Default

	m.authConfig.UpsertAccount(account)
	if hadAccounts && previousDefault != "" && previousDefault != account.User {
		if err := m.authConfig.SetDefault(previousDefault); err != nil {
			*m.authConfig = previous
			return err
		}
	}

	if err := config.SaveAuth(*m.authConfig); err != nil {
		*m.authConfig = previous
		return fmt.Errorf("save auth config: %w", err)
	}
	return nil
}

func cloneAuthConfig(auth config.AuthConfig) config.AuthConfig {
	accounts := make([]config.Account, len(auth.Accounts))
	copy(accounts, auth.Accounts)
	return config.AuthConfig{
		Default:  auth.Default,
		Accounts: accounts,
	}
}

func (m *MonitorModel) applySettingsCmd(candidate *config.Settings) tea.Cmd {
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
	if m.collector == nil {
		return m.handleCurrentViewKey(msg)
	}

	switch m.state {
	case tui.ViewStats:
		if resetter, ok := m.collector.(monitor.StatsResetter); ok {
			resetter.ResetStats()
		} else {
			m.collector.Reset()
		}
		m.snapshot = m.collector.Snapshot()
		m.sharedState.Snapshot = m.snapshot
		m.setStatus(tui.ViewStats, "Stats counters cleared")
		return m, nil
	case tui.ViewActivity:
		return m.handleCurrentViewKey(msg)
	case tui.ViewModels:
		return m.handleCurrentViewKey(msg)
	case tui.ViewLogs:
		m.collector.Reset()
	}
	return m.handleCurrentViewKey(msg)
}

func (m *MonitorModel) handleRefresh() (tea.Model, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		return m, m.beginUserInfoRefresh()
	case tui.ViewModels:
		m.loading = true
		m.setStatus(tui.ViewModels, "Refreshing models...")
		cmd := m.loadModelsCmd()
		return m, cmd
	case tui.ViewActivity:
		return m, nil
	case tui.ViewLogs:
		return m, nil
	default:
		return m, nil
	}
}

func (m *MonitorModel) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.statsView.SetSize(msg.Width, msg.Height)
	m.activityView.SetSize(msg.Width, msg.Height)
	m.modelsView.SetSize(msg.Width, msg.Height)
	m.logsView.SetSize(msg.Width, msg.Height)
	m.sharedState.Width = msg.Width
	m.sharedState.Height = msg.Height
}

func (m *MonitorModel) handleTick() (tea.Model, tea.Cmd) {
	if m.collector != nil {
		m.snapshot = m.collector.Snapshot()
		m.sharedState.Snapshot = m.snapshot
	}
	return m, tickCmd()
}

func (m *MonitorModel) handleModelsLoaded(msg modelsLoadedMsg) {
	m.loading = false
	if msg.err == nil {
		m.models = msg.models
		m.sharedState.Models = msg.models
		m.setStatus(tui.ViewModels, fmt.Sprintf("Loaded %d models", len(msg.models)))
		m.lastRefresh = time.Now()
		m.loadedModels = true
		m.modelsView.SetModels(msg.models)
		return
	}
	m.setStatus(tui.ViewModels, fmt.Sprintf("Models: %v", msg.err))
}

func (m *MonitorModel) handleUserInfoLoaded(msg userInfoLoadedMsg) {
	m.loading = false
	if msg.err == nil {
		m.userInfo = msg.info
		m.setStatus(tui.ViewStats, "User info updated")
		m.loadedUserInfo = true
		m.sharedState.UserInfo = msg.info
		return
	}
	m.setStatus(tui.ViewStats, fmt.Sprintf("User: %v", msg.err))
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
	if msg.settings.ListenAddr != "" {
		m.serverAddr = msg.settings.ListenAddr
	}
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
	case tui.ViewActivity:
		return nil
	case tui.ViewLogs:
		return nil
	}
	return nil
}

func (m *MonitorModel) currentViewHandleKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch m.state {
	case tui.ViewStats:
		return m.statsView.HandleKey(msg)
	case tui.ViewActivity:
		return m.activityView.HandleKey(msg)
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
	case tui.ViewActivity:
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

	var content string
	switch m.state {
	case tui.ViewStats:
		content = m.statsView.View()
	case tui.ViewModels:
		content = m.modelsView.View()
	case tui.ViewActivity:
		content = m.activityView.View()
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
	tabNames := []string{"Stats", "Activity", "Models", "Logs"}
	viewOrder := []tui.ViewState{tui.ViewStats, tui.ViewActivity, tui.ViewModels, tui.ViewLogs}
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
	helpView := m.help.View(&m.keys)
	return tui.DimStyle.Render(helpView)
}

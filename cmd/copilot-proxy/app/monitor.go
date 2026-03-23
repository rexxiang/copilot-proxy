package app

import (
	"context"
	"errors"
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

func (m *MonitorModel) handleQuit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

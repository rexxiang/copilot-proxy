package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
	core "copilot-proxy/internal/core"
	accountcore "copilot-proxy/internal/core/account"
	"copilot-proxy/internal/core/runtimeapi"
)

const defaultPremiumTTL = 30 * time.Second

var (
	ErrNoLoginSession       = errors.New("account: login session not found")
	ErrLoginSessionMismatch = errors.New("account: login session mismatch")
)

type AccountManagerDeps struct {
	LoadAuth      func() (config.AuthConfig, error)
	SaveAuth      func(config.AuthConfig) error
	RequestCode   func(ctx context.Context) (auth.DeviceCodeResponse, error)
	PollToken     func(ctx context.Context, device auth.DeviceCodeResponse) (string, error)
	FetchLogin    func(ctx context.Context, token string) (string, error)
	FetchUserInfo func(ctx context.Context, token string) (*core.UserInfo, error)
	Now           func() time.Time
	PremiumTTL    time.Duration
}

type AccountManager struct {
	mu            sync.Mutex
	loadAuth      func() (config.AuthConfig, error)
	saveAuth      func(config.AuthConfig) error
	requestCode   func(ctx context.Context) (auth.DeviceCodeResponse, error)
	pollToken     func(ctx context.Context, device auth.DeviceCodeResponse) (string, error)
	fetchLogin    func(ctx context.Context, token string) (string, error)
	fetchUserInfo func(ctx context.Context, token string) (*core.UserInfo, error)
	now           func() time.Time
	premiumTTL    time.Duration
	nextSession   int64
	loginSession  *accountLoginSession
	premiumCache  map[string]premiumCacheEntry
}

type accountLoginSession struct {
	id     int64
	ctx    context.Context
	cancel context.CancelFunc
	device auth.DeviceCodeResponse
}

type premiumCacheEntry struct {
	info      core.UserInfo
	retrieved time.Time
}

func NewAccountManager(deps AccountManagerDeps) *AccountManager {
	nowFn := deps.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	premiumTTL := deps.PremiumTTL
	if premiumTTL <= 0 {
		premiumTTL = defaultPremiumTTL
	}
	return &AccountManager{
		loadAuth:      deps.LoadAuth,
		saveAuth:      deps.SaveAuth,
		requestCode:   deps.RequestCode,
		pollToken:     deps.PollToken,
		fetchLogin:    deps.FetchLogin,
		fetchUserInfo: deps.FetchUserInfo,
		now:           nowFn,
		premiumTTL:    premiumTTL,
		premiumCache:  make(map[string]premiumCacheEntry),
	}
}

func (m *AccountManager) List() []accountcore.AccountDTO {
	authConfig, err := m.loadCurrentAuth()
	if err != nil {
		return nil
	}
	accounts := make([]accountcore.AccountDTO, len(authConfig.Accounts))
	for i, acct := range authConfig.Accounts {
		accounts[i] = accountcore.AccountDTO{
			User:      acct.User,
			AppID:     acct.AppID,
			HasToken:  acct.GhToken != "",
			IsDefault: acct.User == authConfig.Default,
		}
	}
	return accounts
}

func (m *AccountManager) Current() (config.Account, bool, error) {
	authConfig, err := m.loadCurrentAuth()
	if err != nil {
		return config.Account{}, false, err
	}
	return authConfig.DefaultAccount()
}

func (m *AccountManager) SwitchDefault(user string) error {
	authConfig, err := m.loadCurrentAuth()
	if err != nil {
		return err
	}
	if err := accountcore.ActivateDefaultAccount(&authConfig, user, m.saveAuth); err != nil {
		return err
	}
	m.InvalidatePremium(user)
	return nil
}

func (m *AccountManager) Add(account config.Account) error {
	authConfig, err := m.loadCurrentAuth()
	if err != nil {
		return err
	}
	return accountcore.UpsertAccountPreserveDefault(&authConfig, account, m.saveAuth)
}

func (m *AccountManager) Remove(user string) error {
	authConfig, err := m.loadCurrentAuth()
	if err != nil {
		return err
	}
	if removed := authConfig.RemoveAccount(user); !removed {
		return config.ErrAccountNotFound
	}
	if err := m.saveAuth(authConfig); err != nil {
		return err
	}
	m.InvalidatePremium(user)
	return nil
}

func (m *AccountManager) BeginLogin(ctx context.Context) (accountcore.LoginChallenge, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m.requestCode == nil {
		return accountcore.LoginChallenge{}, errors.New("request code callback is required")
	}
	loginCtx, cancel := context.WithCancel(ctx)
	device, err := m.requestCode(loginCtx)
	if err != nil {
		cancel()
		return accountcore.LoginChallenge{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginSession != nil {
		m.loginSession.cancel()
	}
	m.nextSession++
	m.loginSession = &accountLoginSession{
		id:     m.nextSession,
		ctx:    loginCtx,
		cancel: cancel,
		device: device,
	}
	return m.challengeFromSession(m.loginSession), nil
}

func (m *AccountManager) PollLogin(ctx context.Context, seq int64) (accountcore.LoginResult, error) {
	session, err := m.sessionByID(seq)
	if err != nil {
		return accountcore.LoginResult{}, err
	}
	if m.pollToken == nil {
		return accountcore.LoginResult{Seq: seq}, errors.New("poll token callback is required")
	}
	if m.fetchLogin == nil {
		return accountcore.LoginResult{Seq: seq}, errors.New("fetch login callback is required")
	}

	mergedCtx, cancel := mergeAccountContext(ctx, session.ctx)
	defer cancel()

	token, err := m.pollToken(mergedCtx, session.device)
	if err != nil {
		return accountcore.LoginResult{Seq: seq}, err
	}
	login, err := m.fetchLogin(mergedCtx, token)
	if err != nil {
		return accountcore.LoginResult{Seq: seq, Token: token}, err
	}
	m.InvalidatePremium(login)

	m.mu.Lock()
	if m.loginSession != nil && m.loginSession.id == seq {
		m.loginSession = nil
	}
	m.mu.Unlock()

	return accountcore.LoginResult{Seq: seq, Token: token, Login: login}, nil
}

func (m *AccountManager) CancelLogin(seq int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginSession == nil {
		return
	}
	if seq != 0 && m.loginSession.id != seq {
		return
	}
	m.loginSession.cancel()
	m.loginSession = nil
}

func (m *AccountManager) PremiumInfo(ctx context.Context, force bool) (core.UserInfo, error) {
	if m.fetchUserInfo == nil {
		return core.UserInfo{}, errors.New("fetch user info callback is required")
	}
	account, _, err := m.Current()
	if err != nil {
		return core.UserInfo{}, err
	}
	if account.User == "" {
		return core.UserInfo{}, config.ErrAccountNotFound
	}

	now := m.now()
	m.mu.Lock()
	entry, ok := m.premiumCache[account.User]
	if ok && !force && now.Sub(entry.retrieved) < m.premiumTTL {
		info := entry.info
		m.mu.Unlock()
		return info, nil
	}
	m.mu.Unlock()

	info, err := m.fetchUserInfo(ctx, account.GhToken)
	if err != nil {
		return core.UserInfo{}, err
	}
	var resolved core.UserInfo
	if info != nil {
		resolved = *info
	}
	m.mu.Lock()
	m.premiumCache[account.User] = premiumCacheEntry{info: resolved, retrieved: now}
	m.mu.Unlock()
	return resolved, nil
}

func (m *AccountManager) InvalidatePremium(user string) {
	if user == "" {
		return
	}
	m.mu.Lock()
	delete(m.premiumCache, user)
	m.mu.Unlock()
}

func (m *AccountManager) loadCurrentAuth() (config.AuthConfig, error) {
	if m.loadAuth == nil {
		return config.AuthConfig{}, errors.New("load auth callback is required")
	}
	authConfig, err := m.loadAuth()
	if err != nil {
		return config.AuthConfig{}, err
	}
	return authConfig, nil
}

func (m *AccountManager) sessionByID(seq int64) (*accountLoginSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginSession == nil {
		return nil, ErrNoLoginSession
	}
	if m.loginSession.id != seq {
		return nil, ErrLoginSessionMismatch
	}
	return m.loginSession, nil
}

func (m *AccountManager) challengeFromSession(session *accountLoginSession) accountcore.LoginChallenge {
	expiresAt := time.Time{}
	if session.device.ExpiresIn > 0 {
		expiresAt = m.now().Add(time.Duration(session.device.ExpiresIn) * time.Second)
	}
	interval := time.Duration(session.device.Interval) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	return accountcore.LoginChallenge{
		Seq:             session.id,
		DeviceCode:      session.device.DeviceCode,
		UserCode:        session.device.UserCode,
		VerificationURI: session.device.VerificationURI,
		Interval:        interval,
		ExpiresAt:       expiresAt,
	}
}

func mergeAccountContext(primary, linked context.Context) (context.Context, context.CancelFunc) {
	switch {
	case primary == nil && linked == nil:
		return context.WithCancel(context.Background())
	case primary == nil:
		return context.WithCancel(linked)
	case linked == nil:
		return context.WithCancel(primary)
	default:
		ctx, cancel := context.WithCancel(primary)
		go func() {
			select {
			case <-linked.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
		return ctx, cancel
	}
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		return http.DefaultClient
	}
	return &http.Client{Timeout: timeout}
}

func newRuntimeAccountManager(
	loadAuth func() (config.AuthConfig, error),
	saveAuth func(config.AuthConfig) error,
	loadSettings func() (config.Settings, error),
	httpClient *http.Client,
) *AccountManager {
	settingsLoader := loadSettings
	if settingsLoader == nil {
		settingsLoader = config.LoadSettings
	}
	api := runtimeapi.NewRuntime(runtimeapi.Options{
		SettingsProvider: func(context.Context) (config.Settings, error) {
			return settingsLoader()
		},
		HTTPClientFactory: func() *http.Client {
			if httpClient != nil {
				return httpClient
			}
			return http.DefaultClient
		},
	})
	return NewAccountManager(AccountManagerDeps{
		LoadAuth: loadAuth,
		SaveAuth: saveAuth,
		RequestCode: func(ctx context.Context) (auth.DeviceCodeResponse, error) {
			return api.RequestCode(ctx)
		},
		PollToken: func(ctx context.Context, device auth.DeviceCodeResponse) (string, error) {
			return api.PollToken(ctx, device)
		},
		FetchLogin: func(ctx context.Context, token string) (string, error) {
			return api.FetchLogin(ctx, token)
		},
		FetchUserInfo: func(ctx context.Context, token string) (*core.UserInfo, error) {
			return api.FetchUserInfo(ctx, token)
		},
	})
}

package account

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"
)

var (
	ErrNoLoginSession       = errors.New("account: login session not found")
	ErrLoginSessionMismatch = errors.New("account: login session mismatch")
)

// AccountDTO exposes account metadata for UI/exports.
type AccountDTO struct {
	User      string
	AppID     string
	HasToken  bool
	IsDefault bool
}

// LoginChallenge describes the device flow challenge returned to users.
type LoginChallenge struct {
	Seq             int64         // Unique session identifier
	DeviceCode      string        // Device code returned by GitHub
	UserCode        string        // Short code users type in
	VerificationURI string        // URL to visit
	Interval        time.Duration // Poll interval
	ExpiresAt       time.Time     // Calculated expiration time
}

// LoginResult contains the token and login retrieved after polling.
type LoginResult struct {
	Seq   int64
	Token string
	Login string
}

// deviceFlow abstracts the GitHub device flow interactions.
type deviceFlow interface {
	RequestCodeWithContext(context.Context) (auth.DeviceCodeResponse, error)
	PollAccessTokenWithContext(context.Context, auth.DeviceCodeResponse) (string, error)
}

// FetchUserFunc resolves a GitHub login given a token.
type FetchUserFunc func(ctx context.Context, client *http.Client, apiBaseURL, ghToken string) (string, error)

// Option configures an account service instance.
type Option func(*Service)

// WithPremiumService overrides the premium cache implementation.
func WithPremiumService(premium *PremiumService) Option {
	return func(s *Service) {
		s.premium = premium
	}
}

// WithDeviceFlowFactory overrides the device flow factory.
func WithDeviceFlowFactory(factory func() deviceFlow) Option {
	return func(s *Service) {
		s.makeFlow = factory
	}
}

// WithFetchUserFunc overrides the GitHub user lookup function.
func WithFetchUserFunc(fetch FetchUserFunc) Option {
	return func(s *Service) {
		s.fetchUser = fetch
	}
}

// WithHTTPClient overrides the HTTP client used for user lookups.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		s.httpClient = client
	}
}

// WithSaveAuthFunc overrides the persistence hook.
func WithSaveAuthFunc(save func(config.AuthConfig) error) Option {
	return func(s *Service) {
		s.save = save
	}
}

// Service manages account state and premium interactions.
type Service struct {
	mu           sync.Mutex
	auth         config.AuthConfig
	premium      *PremiumService
	save         func(config.AuthConfig) error
	makeFlow     func() deviceFlow
	fetchUser    FetchUserFunc
	httpClient   *http.Client
	loginSession *loginSession
	nextSession  int64
}

type loginSession struct {
	id     int64
	ctx    context.Context
	cancel context.CancelFunc
	device auth.DeviceCodeResponse
}

// New constructs an account service with optional overrides.
func New(auth config.AuthConfig, opts ...Option) *Service {
	svc := &Service{
		auth:       auth,
		premium:    NewPremiumService(nil, ""),
		save:       config.SaveAuth,
		makeFlow:   defaultDeviceFlow,
		fetchUser:  defaultFetchUser,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(svc)
	}
	if svc.premium == nil {
		svc.premium = NewPremiumService(nil, "")
	}
	if svc.save == nil {
		svc.save = config.SaveAuth
	}
	if svc.makeFlow == nil {
		svc.makeFlow = defaultDeviceFlow
	}
	if svc.fetchUser == nil {
		svc.fetchUser = defaultFetchUser
	}
	if svc.httpClient == nil {
		svc.httpClient = http.DefaultClient
	}
	return svc
}

// List returns a snapshot of configured accounts.
func (s *Service) List() []AccountDTO {
	s.mu.Lock()
	defer s.mu.Unlock()

	accounts := make([]AccountDTO, len(s.auth.Accounts))
	for i, acct := range s.auth.Accounts {
		accounts[i] = AccountDTO{
			User:      acct.User,
			AppID:     acct.AppID,
			HasToken:  acct.GhToken != "",
			IsDefault: acct.User == s.auth.Default,
		}
	}
	return accounts
}

// Current returns the active account and indicates whether the default flag changed.
func (s *Service) Current() (config.Account, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth.DefaultAccount()
}

// SwitchDefault changes which account is active.
func (s *Service) SwitchDefault(user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ActivateDefaultAccount(&s.auth, user, s.saveAuth); err != nil {
		return err
	}
	s.invalidatePremiumLocked(user)
	return nil
}

// Add persists a new account without touching the default unnecessarily.
func (s *Service) Add(account config.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return UpsertAccountPreserveDefault(&s.auth, account, s.saveAuth)
}

// Remove deletes an account and drops premium cache for that user.
func (s *Service) Remove(user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if removed := s.auth.RemoveAccount(user); !removed {
		return config.ErrAccountNotFound
	}
	s.premium.Invalidate(user)
	return s.saveAuth(s.auth)
}

// PremiumInfo fetches the cached user info or refreshes when forced.
func (s *Service) PremiumInfo(ctx context.Context, force bool) (monitor.UserInfo, error) {
	account, _, err := s.Current()
	if err != nil {
		return monitor.UserInfo{}, err
	}
	return s.premium.Get(ctx, account, force)
}

// InvalidatePremium clears cached premium metadata for a user.
func (s *Service) InvalidatePremium(user string) {
	s.premium.Invalidate(user)
}

// BeginLogin requests a device code and returns the challenge metadata.
func (s *Service) BeginLogin(ctx context.Context) (LoginChallenge, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	flow := s.makeFlow()
	loginCtx, loginCancel := context.WithCancel(ctx)
	device, err := flow.RequestCodeWithContext(loginCtx)
	if err != nil {
		loginCancel()
		return LoginChallenge{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelSessionLocked()
	s.nextSession++
	session := &loginSession{
		id:     s.nextSession,
		ctx:    loginCtx,
		cancel: loginCancel,
		device: device,
	}
	s.loginSession = session
	return challengeFromSession(session), nil
}

// PollLogin exchanges the device code for a token and login.
func (s *Service) PollLogin(ctx context.Context, seq int64) (LoginResult, error) {
	session, err := s.sessionByID(seq)
	if err != nil {
		return LoginResult{}, err
	}

	flow := s.makeFlow()
	mergedCtx, cancel := mergeContext(ctx, session.ctx)
	defer cancel()

	token, err := flow.PollAccessTokenWithContext(mergedCtx, session.device)
	if err != nil {
		return LoginResult{Seq: seq}, err
	}

	login, err := s.fetchUser(mergedCtx, s.httpClient, config.GitHubAPIURL, token)
	if err != nil {
		return LoginResult{Seq: seq, Token: token}, err
	}

	s.invalidatePremiumLocked(login)

	s.mu.Lock()
	if s.loginSession != nil && s.loginSession.id == seq {
		s.loginSession = nil
	}
	s.mu.Unlock()

	return LoginResult{Seq: seq, Token: token, Login: login}, nil
}

// CancelLogin cancels an in-flight device flow for the provided session.
// If seq is zero it cancels whatever session is active.
func (s *Service) CancelLogin(seq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginSession == nil {
		return
	}
	if seq != 0 && s.loginSession.id != seq {
		return
	}
	s.loginSession.cancel()
	s.loginSession = nil
}

func (s *Service) sessionByID(seq int64) (*loginSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginSession == nil {
		return nil, ErrNoLoginSession
	}
	if s.loginSession.id != seq {
		return nil, ErrLoginSessionMismatch
	}
	return s.loginSession, nil
}

func (s *Service) cancelSessionLocked() {
	if s.loginSession == nil {
		return
	}
	s.loginSession.cancel()
	s.loginSession = nil
}

func (s *Service) saveAuth(next config.AuthConfig) error {
	if err := s.save(next); err != nil {
		return err
	}
	s.auth = next
	return nil
}

func (s *Service) invalidatePremiumLocked(user string) {
	if user == "" {
		return
	}
	s.premium.Invalidate(user)
}

func challengeFromSession(session *loginSession) LoginChallenge {
	expiresAt := time.Time{}
	if session.device.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(session.device.ExpiresIn) * time.Second)
	}
	interval := time.Duration(session.device.Interval) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	return LoginChallenge{
		Seq:             session.id,
		DeviceCode:      session.device.DeviceCode,
		UserCode:        session.device.UserCode,
		VerificationURI: session.device.VerificationURI,
		Interval:        interval,
		ExpiresAt:       expiresAt,
	}
}

func mergeContext(primary, linked context.Context) (context.Context, context.CancelFunc) {
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

func defaultDeviceFlow() deviceFlow {
	return auth.DeviceFlow{
		ClientID: config.OAuthClientID,
		Scope:    config.OAuthScope,
		BaseURL:  config.GitHubBaseURL,
	}
}

func defaultFetchUser(ctx context.Context, client *http.Client, apiBaseURL, ghToken string) (string, error) {
	return auth.FetchUserWithContext(ctx, client, apiBaseURL, ghToken)
}

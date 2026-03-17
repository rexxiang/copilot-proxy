package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"copilot-proxy/internal/runtime/config"
)

var (
	ErrAccessDenied       = errors.New("access denied")
	ErrDeviceCodeExpired  = errors.New("device code expired")
	ErrDeviceCodeMissing  = errors.New("device code missing")
	ErrHTTPRequestFailed  = errors.New("http request failed")
	ErrMissingGitHubToken = errors.New("missing GitHub token")
	ErrMissingUserLogin   = errors.New("missing user login")
	ErrNilContext         = errors.New("nil context")
	ErrUserLookupFailed   = errors.New("user lookup failed")
)

type DeviceFlow struct {
	ClientID   string
	Scope      string
	BaseURL    string
	HTTPClient *http.Client
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

type LoginResult struct {
	Token           string
	VerificationURI string
	UserCode        string
}

type GitHubUser struct {
	Login string `json:"login"`
}

func (d DeviceFlow) Login() (LoginResult, error) {
	return d.LoginWithContext(context.Background())
}

func (d DeviceFlow) LoginWithContext(ctx context.Context) (LoginResult, error) {
	device, err := d.RequestCodeWithContext(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	token, err := d.PollAccessTokenWithContext(ctx, device)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Token:           token,
		VerificationURI: device.VerificationURI,
		UserCode:        device.UserCode,
	}, nil
}

func (d DeviceFlow) RequestCode() (DeviceCodeResponse, error) {
	return d.RequestCodeWithContext(context.Background())
}

func (d DeviceFlow) RequestCodeWithContext(ctx context.Context) (DeviceCodeResponse, error) {
	client := d.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	base := d.BaseURL
	if base == "" {
		base = config.GitHubBaseURL
	}
	clientID := d.ClientID
	if clientID == "" {
		clientID = config.OAuthClientID
	}
	payload := map[string]string{"client_id": clientID}
	if d.Scope != "" {
		payload["scope"] = d.Scope
	}

	device, err := postJSONWithContext[DeviceCodeResponse](ctx, client, base+"/login/device/code", payload)
	if err != nil {
		return DeviceCodeResponse{}, fmt.Errorf("request device code: %w", err)
	}
	if device.DeviceCode == "" {
		return DeviceCodeResponse{}, ErrDeviceCodeMissing
	}
	return device, nil
}

func (d DeviceFlow) PollAccessToken(device DeviceCodeResponse) (string, error) {
	return d.PollAccessTokenWithContext(context.Background(), device)
}

func (d DeviceFlow) PollAccessTokenWithContext(ctx context.Context, device DeviceCodeResponse) (string, error) {
	client := d.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	base := d.BaseURL
	if base == "" {
		base = config.GitHubBaseURL
	}
	clientID := d.ClientID
	if clientID == "" {
		clientID = config.OAuthClientID
	}
	return d.pollAccessTokenWithContext(ctx, client, base, clientID, device)
}

func (d DeviceFlow) pollAccessTokenWithContext(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	clientID string,
	device DeviceCodeResponse,
) (string, error) {
	if ctx == nil {
		return "", ErrNilContext
	}
	interval := time.Duration(device.Interval+1) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)

	payload := map[string]string{
		"client_id":   clientID,
		"device_code": device.DeviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}

	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("device code polling canceled: %w", err)
		}
		if time.Now().After(deadline) {
			return "", ErrDeviceCodeExpired
		}
		resp, err := postJSONWithContext[AccessTokenResponse](ctx, client, baseURL+"/login/oauth/access_token", payload)
		if err == nil {
			if resp.AccessToken != "" {
				return resp.AccessToken, nil
			}
			switch resp.Error {
			case "slow_down":
				interval += time.Second
			case "access_denied":
				return "", ErrAccessDenied
			case "expired_token":
				return "", ErrDeviceCodeExpired
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C // Drain the channel to prevent goroutine leak
			}
			return "", fmt.Errorf("device code polling canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func FetchUser(client *http.Client, apiBaseURL, ghToken string) (string, error) {
	return FetchUserWithContext(context.Background(), client, apiBaseURL, ghToken)
}

func FetchUserWithContext(ctx context.Context, client *http.Client, apiBaseURL, ghToken string) (string, error) {
	if ghToken == "" {
		return "", ErrMissingGitHubToken
	}
	if ctx == nil {
		return "", ErrNilContext
	}
	if client == nil {
		client = http.DefaultClient
	}
	if apiBaseURL == "" {
		apiBaseURL = config.GitHubAPIURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+"/user", http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create user request: %w", err)
	}
	req.Header.Set("Authorization", "token "+ghToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch user: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: %s", ErrUserLookupFailed, resp.Status)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", fmt.Errorf("decode user response: %w", err)
	}
	if user.Login == "" {
		return "", ErrMissingUserLogin
	}
	return user.Login, nil
}

func postJSONWithContext[T any](ctx context.Context, client *http.Client, url string, payload map[string]string) (T, error) {
	var zero T
	body, err := json.Marshal(payload)
	if err != nil {
		return zero, fmt.Errorf("marshal payload: %w", err)
	}
	if ctx == nil {
		return zero, ErrNilContext
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return zero, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return zero, fmt.Errorf("perform request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Don't expose response body in error message as it may contain sensitive information
		_, _ = io.Copy(io.Discard, resp.Body)
		return zero, fmt.Errorf("%w: %s", ErrHTTPRequestFailed, resp.Status)
	}

	var parsed T
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return parsed, nil
}

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceFlow(t *testing.T) {
	var deviceCalled bool
	var tokenCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			deviceCalled = true
			_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
				DeviceCode:      "device",
				UserCode:        "user",
				VerificationURI: "https://example.com",
				ExpiresIn:       900,
				Interval:        0,
			})
		case "/login/oauth/access_token":
			tokenCalled = true
			_ = json.NewEncoder(w).Encode(AccessTokenResponse{AccessToken: "gh-token"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	flow := DeviceFlow{ClientID: "Iv1.test", Scope: "read:user", BaseURL: srv.URL, HTTPClient: client}
	result, err := flow.Login()
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if result.Token != "gh-token" {
		t.Fatalf("unexpected token: %s", result.Token)
	}
	if !deviceCalled || !tokenCalled {
		t.Fatalf("expected device and token calls")
	}
}

func TestFetchUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gh" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(GitHubUser{Login: "user1"})
	}))
	defer srv.Close()

	client := srv.Client()
	login, err := FetchUser(client, srv.URL, "gh")
	if err != nil {
		t.Fatalf("FetchUser error: %v", err)
	}
	if login != "user1" {
		t.Fatalf("unexpected login: %s", login)
	}
}

func TestRequestCodeAndPollAccessToken(t *testing.T) {
	var deviceCalled bool
	var tokenCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			deviceCalled = true
			_ = json.NewEncoder(w).Encode(DeviceCodeResponse{
				DeviceCode:      "device",
				UserCode:        "user",
				VerificationURI: "https://example.com",
				ExpiresIn:       900,
				Interval:        0,
			})
		case "/login/oauth/access_token":
			tokenCalled = true
			_ = json.NewEncoder(w).Encode(AccessTokenResponse{AccessToken: "gh-token"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	flow := DeviceFlow{ClientID: "Iv1.test", Scope: "read:user", BaseURL: srv.URL, HTTPClient: srv.Client()}
	device, err := flow.RequestCode()
	if err != nil {
		t.Fatalf("RequestCode error: %v", err)
	}
	if device.DeviceCode != "device" {
		t.Fatalf("unexpected device code: %s", device.DeviceCode)
	}

	token, err := flow.PollAccessToken(device)
	if err != nil {
		t.Fatalf("PollAccessToken error: %v", err)
	}
	if token != "gh-token" {
		t.Fatalf("unexpected token: %s", token)
	}
	if !deviceCalled || !tokenCalled {
		t.Fatalf("expected device and token calls")
	}
}

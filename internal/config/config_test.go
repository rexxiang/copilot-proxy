package config

import (
	"reflect"
	"testing"
)

func TestLoadAuthMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	auth, err := LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth error: %v", err)
	}
	if len(auth.Accounts) != 0 {
		t.Fatalf("expected no accounts")
	}
	if auth.Default != "" {
		t.Fatalf("expected empty default")
	}
}

func TestSaveLoadAuth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	input := AuthConfig{
		Default: "user-1",
		Accounts: []Account{
			{User: "user-1", GhToken: "token-1", AppID: "app-1"},
			{User: "user-2", GhToken: "token-2", AppID: "app-2"},
		},
	}

	if err := SaveAuth(input); err != nil {
		t.Fatalf("SaveAuth error: %v", err)
	}

	output, err := LoadAuth()
	if err != nil {
		t.Fatalf("LoadAuth error: %v", err)
	}
	if !reflect.DeepEqual(input, output) {
		t.Fatalf("auth mismatch: %#v != %#v", input, output)
	}
}

func TestEnsureDefault(t *testing.T) {
	auth := AuthConfig{
		Accounts: []Account{{User: "u1"}, {User: "u2"}},
	}

	changed := auth.EnsureDefault()
	if !changed {
		t.Fatalf("expected EnsureDefault to change default")
	}
	if auth.Default != "u1" {
		t.Fatalf("expected default u1, got %s", auth.Default)
	}

	changed = auth.EnsureDefault()
	if changed {
		t.Fatalf("expected no change on second call")
	}
}

func TestDefaultAccount(t *testing.T) {
	auth := AuthConfig{
		Default:  "missing",
		Accounts: []Account{{User: "u1"}},
	}

	acct, changed, err := auth.DefaultAccount()
	if err != nil {
		t.Fatalf("DefaultAccount error: %v", err)
	}
	if !changed {
		t.Fatalf("expected default to be updated")
	}
	if acct.User != "u1" {
		t.Fatalf("unexpected user: %s", acct.User)
	}

	empty := AuthConfig{}
	_, _, err = empty.DefaultAccount()
	if err == nil {
		t.Fatalf("expected error for empty auth")
	}
}

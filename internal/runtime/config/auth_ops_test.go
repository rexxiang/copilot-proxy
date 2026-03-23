package config

import "testing"

func TestUpsertAccountAddsAndSetsDefault(t *testing.T) {
	auth := AuthConfig{}
	acct := Account{User: "u1", GhToken: "t1", AppID: "app1"}

	auth.UpsertAccount(acct)

	if auth.Default != "u1" {
		t.Fatalf("expected default u1, got %s", auth.Default)
	}
	if len(auth.Accounts) != 1 {
		t.Fatalf("expected 1 account")
	}
	if auth.Accounts[0].GhToken != "t1" {
		t.Fatalf("token not set")
	}
}

func TestUpsertAccountUpdatesExisting(t *testing.T) {
	auth := AuthConfig{
		Default:  "u1",
		Accounts: []Account{{User: "u1", GhToken: "old", AppID: "old"}},
	}

	auth.UpsertAccount(Account{User: "u1", GhToken: "new", AppID: "app"})

	if len(auth.Accounts) != 1 {
		t.Fatalf("expected 1 account")
	}
	if auth.Accounts[0].GhToken != "new" {
		t.Fatalf("expected updated token")
	}
	if auth.Default != "u1" {
		t.Fatalf("default should remain u1")
	}
}

func TestRemoveAccount(t *testing.T) {
	auth := AuthConfig{
		Default:  "u1",
		Accounts: []Account{{User: "u1"}, {User: "u2"}},
	}

	removed := auth.RemoveAccount("u1")
	if !removed {
		t.Fatalf("expected remove true")
	}
	if len(auth.Accounts) != 1 || auth.Accounts[0].User != "u2" {
		t.Fatalf("unexpected remaining accounts")
	}
	if auth.Default != "u2" {
		t.Fatalf("default should move to u2")
	}

	removed = auth.RemoveAccount("missing")
	if removed {
		t.Fatalf("expected remove false")
	}
}

func TestSetDefault(t *testing.T) {
	auth := AuthConfig{Accounts: []Account{{User: "u1"}, {User: "u2"}}}
	if err := auth.SetDefault("u2"); err != nil {
		t.Fatalf("SetDefault error: %v", err)
	}
	if auth.Default != "u2" {
		t.Fatalf("expected default u2")
	}
	if err := auth.SetDefault("missing"); err == nil {
		t.Fatalf("expected error for missing user")
	}
}

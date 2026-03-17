package tui

import (
	"strings"
	"testing"

	"copilot-proxy/internal/runtime/identity/account"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAccountModalOpenDefaultsToActiveAccount(t *testing.T) {
	accounts := []account.AccountDTO{
		{User: "u1"},
		{User: "u2", IsDefault: true},
		{User: "u3"},
	}

	modal := NewAccountModal()
	if err := modal.Open(accounts, "u2"); err != nil {
		t.Fatalf("open modal: %v", err)
	}
	if !modal.IsOpen() {
		t.Fatalf("expected modal to be open")
	}
	if got := modal.SelectedUser(); got != "u2" {
		t.Fatalf("expected selected user u2, got %q", got)
	}
	if view := modal.View(); !strings.Contains(view, "Add Account") {
		t.Fatalf("expected Add Account row in view, got:\n%s", view)
	}
}

func TestAccountModalHandleKeyNavigationAndActions(t *testing.T) {
	modal := NewAccountModal()
	if err := modal.Open([]account.AccountDTO{
		{User: "u1", IsDefault: true},
		{User: "u2"},
	}, "u1"); err != nil {
		t.Fatalf("open modal: %v", err)
	}

	action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	if action != AccountModalActionNone {
		t.Fatalf("expected no action on down key, got %v", action)
	}
	if got := modal.SelectedUser(); got != "u2" {
		t.Fatalf("expected selected user u2 after down, got %q", got)
	}

	action = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != AccountModalActionActivate {
		t.Fatalf("expected activate action on enter, got %v", action)
	}

	// Move to Add Account row.
	action = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	if action != AccountModalActionNone {
		t.Fatalf("expected no action on down key to add row, got %v", action)
	}
	action = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != AccountModalActionAdd {
		t.Fatalf("expected add action on add row enter, got %v", action)
	}

	action = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if action != AccountModalActionClose {
		t.Fatalf("expected close action on esc, got %v", action)
	}
}

func TestAccountModalOpenWithNoAccountsStillOpensAddRow(t *testing.T) {
	modal := NewAccountModal()
	if err := modal.Open(nil, ""); err != nil {
		t.Fatalf("open modal with empty auth: %v", err)
	}
	if !modal.IsOpen() {
		t.Fatalf("expected modal to be open")
	}
	if got := modal.SelectedUser(); got != "" {
		t.Fatalf("expected no selected user, got %q", got)
	}
	view := modal.View()
	if !strings.Contains(view, "Add Account") {
		t.Fatalf("expected Add Account in empty modal view, got:\n%s", view)
	}
	action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != AccountModalActionAdd {
		t.Fatalf("expected add action with only add row, got %v", action)
	}
}

func TestAccountModalAuthorizingEscReturnsCancelAdd(t *testing.T) {
	modal := NewAccountModal()
	if err := modal.Open(nil, ""); err != nil {
		t.Fatalf("open modal: %v", err)
	}
	modal.BeginAddAuth("https://github.com/login/device", "ABCD-EFGH")

	action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if action != AccountModalActionCancelAdd {
		t.Fatalf("expected cancel add action on esc in authorizing mode, got %v", action)
	}
	view := modal.View()
	if !strings.Contains(view, "Waiting for authorization") {
		t.Fatalf("expected authorizing view, got:\n%s", view)
	}
}

func TestAccountModalViewShowsActiveMarkerAndError(t *testing.T) {
	modal := NewAccountModal()
	if err := modal.Open([]account.AccountDTO{
		{User: "active-user", IsDefault: true},
		{User: "other-user"},
	}, "active-user"); err != nil {
		t.Fatalf("open modal: %v", err)
	}
	modal.SetError("activate failed")

	view := modal.View()
	if !strings.Contains(view, "active-user (active)") {
		t.Fatalf("expected active marker in view, got:\n%s", view)
	}
	if !strings.Contains(view, "activate failed") {
		t.Fatalf("expected error message in view, got:\n%s", view)
	}
}

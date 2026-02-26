package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

var ansiCodePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestConfigModal_OpenUsesVisibleFieldSpecs(t *testing.T) {
	modal := NewConfigModal()

	settings := config.Settings{
		ListenAddr:      "127.0.0.1:4000",
		UpstreamBase:    "https://api.githubcopilot.com",
		RequiredHeaders: map[string]string{"user-agent": "copilot/0.0.400"},
		UpstreamTimeout: config.NewDuration(5 * time.Minute),
		MaxRetries:      3,
		RetryBackoff:    config.NewDuration(time.Second),
	}

	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	keys := modal.VisibleFieldKeys()
	expected := []string{
		"upstream_base",
		"upstream_timeout",
		"max_retries",
		"retry_backoff",
		"required_headers",
	}
	if len(keys) != len(expected) {
		t.Fatalf("unexpected visible key count: got %d want %d", len(keys), len(expected))
	}
	for i := range expected {
		if keys[i] != expected[i] {
			t.Fatalf("unexpected field order at %d: got %q want %q", i, keys[i], expected[i])
		}
	}
}

func TestConfigModal_ReadOnlyFieldCannotBeEdited(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	before := modal.FieldValue("upstream_base")
	action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if action != ModalActionNone {
		t.Fatalf("expected no action, got %v", action)
	}
	after := modal.FieldValue("upstream_base")
	if after != before {
		t.Fatalf("readonly field should stay unchanged: got %q want %q", after, before)
	}
	if modal.IsDirty() {
		t.Fatalf("readonly edit should not mark dirty")
	}
}

func TestConfigModal_EscWithDirtyRequiresConfirm(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // move to upstream_timeout
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if !modal.IsDirty() {
		t.Fatalf("expected modal to be dirty after edit")
	}

	action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if action != ModalActionNone {
		t.Fatalf("expected no close action on first esc, got %v", action)
	}
	if !modal.InDiscardConfirm() {
		t.Fatalf("expected discard confirm mode")
	}

	action = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if action != ModalActionClose {
		t.Fatalf("expected close action after confirm enter, got %v", action)
	}
}

func TestConfigModal_BuildCandidateFromEditedForm(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // upstream_timeout
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // max_retries
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if candidate.MaxRetries != 36 {
		t.Fatalf("unexpected max retries: got %d want 36", candidate.MaxRetries)
	}
}

func TestConfigModal_KeyValueAddDelete(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnd}) // jump to required_headers
	if modal.CurrentFieldKey() != "required_headers" {
		t.Fatalf("expected required_headers focused, got %q", modal.CurrentFieldKey())
	}

	if modal.KeyValueRowCount("required_headers") != 1 {
		t.Fatalf("expected one default kv row for empty map")
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if modal.KeyValueRowCount("required_headers") != 2 {
		t.Fatalf("expected two kv rows after ctrl+n")
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if modal.KeyValueRowCount("required_headers") != 1 {
		t.Fatalf("expected one kv row after ctrl+d")
	}
}

func TestConfigModal_OpenEmptyMapAddsDefaultRow(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if got := modal.KeyValueRowCount("required_headers"); got != 1 {
		t.Fatalf("expected one default row for empty map, got %d", got)
	}
}

func TestConfigModal_ViewShowsCursorForEditableField(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // upstream_timeout

	view := stripANSI(modal.View())
	if !strings.Contains(view, "Timeout") || !strings.Contains(view, "[5m0s") {
		t.Fatalf("expected timeout input box in view, got:\n%s", view)
	}
}

func TestConfigModal_ViewRendersKeyValueAsTwoInputs(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})   // required_headers
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlN}) // add row
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x-test")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})

	view := stripANSI(modal.View())
	if !strings.Contains(view, "[x-test] : [1") {
		t.Fatalf("expected key/value dual input row, got:\n%s", view)
	}
}

func TestConfigModal_RealCursorMovementOnScalarInput(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // upstream_timeout
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyLeft})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if got := modal.FieldValue("upstream_timeout"); got != "5m0xs" {
		t.Fatalf("expected cursor-aware insertion, got %q", got)
	}
}

func TestConfigModal_RealCursorMovementOnKeyValueInput(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnd})   // required_headers
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlN}) // add row
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("12")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyLeft})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	base := config.DefaultSettings()
	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if got := candidate.RequiredHeaders["ab"]; got != "1x2" {
		t.Fatalf("expected cursor-aware value edit, got %q", got)
	}
}

func TestConfigModal_BuildCandidateDropsBlankKeyOrValueRows(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnd}) // required_headers default row
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x-keep")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlN}) // second row
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x-drop")})

	base := config.DefaultSettings()
	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if got := candidate.RequiredHeaders["x-keep"]; got != "1" {
		t.Fatalf("expected x-keep=1, got %q", got)
	}
	if _, ok := candidate.RequiredHeaders["x-drop"]; ok {
		t.Fatalf("expected x-drop row to be removed when value is empty")
	}
}

func stripANSI(input string) string {
	return ansiCodePattern.ReplaceAllString(input, "")
}

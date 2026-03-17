package tui

import (
	"reflect"
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
		ListenAddr:                        "127.0.0.1:4000",
		UpstreamBase:                      "https://api.githubcopilot.com",
		MessagesAgentDetectionRequestMode: false,
		MaxRetries:                        3,
		RetryBackoff:                      config.NewDuration(time.Second),
	}

	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	keys := modal.VisibleFieldKeys()
	expected := []string{
		"upstream_base",
		"max_retries",
		"retry_backoff",
		"rate_limit_seconds",
		"messages_agent_detection_request_mode",
		"reasoning_policies_ui",
		"claude_haiku_fallback_models_ui",
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

	if modal.CurrentFieldKey() == "upstream_base" {
		t.Fatalf("readonly field should not receive initial focus")
	}
	for i := 0; i < 32; i++ {
		if modal.CurrentFieldKey() == "upstream_base" {
			t.Fatalf("readonly field should not be selectable")
		}
		_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	}
	if modal.CurrentFieldKey() == "upstream_base" {
		t.Fatalf("readonly field should not be selectable after tab cycle")
	}
	for i := 0; i < 32; i++ {
		if modal.CurrentFieldKey() == "upstream_base" {
			t.Fatalf("readonly field should not be selectable")
		}
		_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	for i := 0; i < 32; i++ {
		if modal.CurrentFieldKey() == "upstream_base" {
			t.Fatalf("readonly field should not be selectable")
		}
		_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if modal.CurrentFieldKey() == "upstream_base" {
		t.Fatalf("readonly field should not be selectable after up/down cycle")
	}
}

func TestConfigModal_EscWithDirtyRequiresConfirm(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

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

	focusFieldByKey(t, modal, "max_retries")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if candidate.MaxRetries != 36 {
		t.Fatalf("unexpected max retries: got %d want 36", candidate.MaxRetries)
	}
}

func TestConfigModal_BuildCandidatePreservesBoolField(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	base.MessagesAgentDetectionRequestMode = true
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if !candidate.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode=true")
	}
}

func TestConfigModal_SpaceTogglesBoolField(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "messages_agent_detection_request_mode")
	if got := modal.FieldValue("messages_agent_detection_request_mode"); got != "true" {
		t.Fatalf("expected initial bool value true, got %q", got)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	if got := modal.FieldValue("messages_agent_detection_request_mode"); got != "false" {
		t.Fatalf("expected bool toggled to false, got %q", got)
	}

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if candidate.MessagesAgentDetectionRequestMode {
		t.Fatalf("expected messages_agent_detection_request_mode=false after one toggle")
	}
}

func TestConfigModal_AgentModeRendersPremiumRequestAndSessionLabels(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "messages_agent_detection_request_mode")
	view := stripANSI(modal.View())
	if !strings.Contains(view, "[premium request]") {
		t.Fatalf("expected premium request label in view, got:\n%s", view)
	}
	if strings.Contains(view, "tail message focused") {
		t.Fatalf("expected no mode description text for Msg Agent Mode, got:\n%s", view)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace})
	view = stripANSI(modal.View())
	if !strings.Contains(view, "[session]") {
		t.Fatalf("expected session label in view after toggle, got:\n%s", view)
	}
}

func TestConfigModal_BoolFieldIgnoresTextInput(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "messages_agent_detection_request_mode")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if got := modal.FieldValue("messages_agent_detection_request_mode"); got != "true" {
		t.Fatalf("expected bool field to ignore text input, got %q", got)
	}
}

func TestConfigModal_ObjectArrayAddDelete(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	if modal.CurrentFieldKey() != "reasoning_policies_ui" {
		t.Fatalf("expected reasoning_policies_ui focused, got %q", modal.CurrentFieldKey())
	}

	if got := len(modal.form.ObjectArrayValues["reasoning_policies_ui"]); got != 1 {
		t.Fatalf("expected one default array row, got %d", got)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := len(modal.form.ObjectArrayValues["reasoning_policies_ui"]); got != 2 {
		t.Fatalf("expected two rows after ctrl+n, got %d", got)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := len(modal.form.ObjectArrayValues["reasoning_policies_ui"]); got != 1 {
		t.Fatalf("expected one row after ctrl+d, got %d", got)
	}
}

func TestConfigModal_ArrayAlwaysKeepsTrailingBlankOnOpen(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	settings.ReasoningPolicies = []config.ReasoningPolicy{
		{Model: "gpt-5-mini", Target: "responses", Effort: "high"},
	}
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	rows := modal.form.ObjectArrayValues["reasoning_policies_ui"]
	if len(rows) != 2 {
		t.Fatalf("expected one data row plus one trailing blank row, got %d", len(rows))
	}
	if !isArrayRowEmpty(rows[len(rows)-1]) {
		t.Fatalf("expected last row to be blank, got %#v", rows[len(rows)-1])
	}
}

func TestConfigModal_ArrayTypingLastBlankAutoAppendsBlank(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt-5-mini")})

	rows := modal.form.ObjectArrayValues["reasoning_policies_ui"]
	if len(rows) != 2 {
		t.Fatalf("expected appended trailing blank row after editing, got %d", len(rows))
	}
	if !isArrayRowEmpty(rows[len(rows)-1]) {
		t.Fatalf("expected last row to be blank, got %#v", rows[len(rows)-1])
	}
}

func TestConfigModal_ArrayDeleteLastRowStillKeepsBlank(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt-5-mini")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown}) // move to trailing blank row
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlD})

	rows := modal.form.ObjectArrayValues["reasoning_policies_ui"]
	if len(rows) != 2 {
		t.Fatalf("expected data row plus trailing blank row after deleting last row, got %d", len(rows))
	}
	if !isArrayRowEmpty(rows[len(rows)-1]) {
		t.Fatalf("expected last row to be blank, got %#v", rows[len(rows)-1])
	}
}

func TestConfigModal_ArrayDeleteOnlyRowRecreatesBlank(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlD})

	rows := modal.form.ObjectArrayValues["reasoning_policies_ui"]
	if len(rows) != 1 {
		t.Fatalf("expected one trailing blank row after deleting only row, got %d", len(rows))
	}
	if !isArrayRowEmpty(rows[0]) {
		t.Fatalf("expected remaining row to be blank, got %#v", rows[0])
	}
}

func TestConfigModal_ArrayUpDownMovesRowCursor(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt-5-mini")})

	cursor := modal.arrayCursors["reasoning_policies_ui"]
	if cursor.row != 0 {
		t.Fatalf("expected initial row cursor 0, got %d", cursor.row)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	cursor = modal.arrayCursors["reasoning_policies_ui"]
	if cursor.row != 1 {
		t.Fatalf("expected row cursor to move down to 1, got %d", cursor.row)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	cursor = modal.arrayCursors["reasoning_policies_ui"]
	if cursor.row != 0 {
		t.Fatalf("expected row cursor to move up to 0, got %d", cursor.row)
	}
}

func TestConfigModal_ArrayVerticalBoundaryMovesFieldFocus(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	if modal.CurrentFieldKey() != "reasoning_policies_ui" {
		t.Fatalf("expected reasoning_policies_ui focused, got %q", modal.CurrentFieldKey())
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
	if modal.CurrentFieldKey() != "messages_agent_detection_request_mode" {
		t.Fatalf("expected focus to move to previous field, got %q", modal.CurrentFieldKey())
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	if modal.CurrentFieldKey() == "reasoning_policies_ui" {
		t.Fatalf("expected focus to leave array field at bottom boundary")
	}
	if modal.CurrentFieldKey() == "upstream_base" {
		t.Fatalf("expected readonly field to be skipped on boundary navigation")
	}
}

func TestConfigModal_CtrlSSaves(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}); action != ModalActionNone {
		t.Fatalf("enter should not save anymore, got %v", action)
	}
	if action := modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlS}); action != ModalActionSave {
		t.Fatalf("ctrl+s should save, got %v", action)
	}
}

func TestConfigModal_ViewShowsCursorForEditableField(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "retry_backoff")

	view := stripANSI(modal.View())
	if !strings.Contains(view, "Backoff") || !strings.Contains(view, "[1s") {
		t.Fatalf("expected backoff input box in view, got:\n%s", view)
	}
}

func TestConfigModal_RealCursorMovementOnScalarInput(t *testing.T) {
	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "retry_backoff")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyLeft})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})

	if got := modal.FieldValue("retry_backoff"); got != "1xs" {
		t.Fatalf("expected cursor-aware insertion, got %q", got)
	}
}

func TestConfigModal_BuildCandidateFromReasoningPolicyArray(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt-5-mini")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("responses")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("high")})

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if len(candidate.ReasoningPolicies) != 1 {
		t.Fatalf("expected one reasoning policy, got %#v", candidate.ReasoningPolicies)
	}
	if candidate.ReasoningPolicies[0].Model != "gpt-5-mini" {
		t.Fatalf("unexpected model: %#v", candidate.ReasoningPolicies[0])
	}
	if candidate.ReasoningPolicies[0].Target != "responses" || candidate.ReasoningPolicies[0].Effort != "high" {
		t.Fatalf("unexpected policy values: %#v", candidate.ReasoningPolicies[0])
	}
}

func TestConfigModal_ReasoningPolicyEnumCyclesWithSpaceAndAltSpace(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt-5-mini")})
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})                            // target
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})            // chat
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})            // responses
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: true}) // back to chat
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})                            // effort
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: true}) // empty reverse -> high

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if len(candidate.ReasoningPolicies) != 1 {
		t.Fatalf("expected one reasoning policy, got %#v", candidate.ReasoningPolicies)
	}
	got := candidate.ReasoningPolicies[0]
	if got.Target != "chat" || got.Effort != "high" {
		t.Fatalf("unexpected enum cycle result: %#v", got)
	}
}

func TestConfigModal_ArrayColumnMoveSupportsCtrlAndAlt(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "reasoning_policies_ui")
	cursor := modal.arrayCursors["reasoning_policies_ui"]
	if cursor.col != 0 {
		t.Fatalf("expected initial col=0, got %d", cursor.col)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlRight})
	cursor = modal.arrayCursors["reasoning_policies_ui"]
	if cursor.col != 1 {
		t.Fatalf("expected ctrl+right to move col=1, got %d", cursor.col)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	cursor = modal.arrayCursors["reasoning_policies_ui"]
	if cursor.col != 2 {
		t.Fatalf("expected alt+right to move col=2, got %d", cursor.col)
	}

	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	cursor = modal.arrayCursors["reasoning_policies_ui"]
	if cursor.col != 1 {
		t.Fatalf("expected alt+left to move col=1, got %d", cursor.col)
	}
}

func TestConfigModal_BuildCandidateTreatsEmptyRateLimitAsZero(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	base.RateLimitSeconds = 0
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "rate_limit_seconds")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if candidate.RateLimitSeconds != 0 {
		t.Fatalf("expected empty rate limit to decode as 0, got %d", candidate.RateLimitSeconds)
	}
}

func TestConfigModal_ArrayCtrlDownReordersRows(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	base.ClaudeHaikuFallbackModels = []string{"gpt-5-mini", "grok-code-fast-1"}
	base.ClaudeHaikuFallbackModelsUI = []config.HaikuFallbackModel{
		{Model: "gpt-5-mini"},
		{Model: "grok-code-fast-1"},
	}
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "claude_haiku_fallback_models_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyCtrlDown})

	rows := modal.form.ObjectArrayValues["claude_haiku_fallback_models_ui"]
	if got := rows[0]["model"]; got != "grok-code-fast-1" {
		t.Fatalf("expected first row to move down, got %#v", rows)
	}
	if got := rows[1]["model"]; got != "gpt-5-mini" {
		t.Fatalf("expected second row to move up, got %#v", rows)
	}

	candidate, err := modal.BuildCandidate(&base)
	if err != nil {
		t.Fatalf("BuildCandidate error: %v", err)
	}
	if want := []string{"grok-code-fast-1", "gpt-5-mini"}; !reflect.DeepEqual(candidate.ClaudeHaikuFallbackModels, want) {
		t.Fatalf("unexpected reordered fallbacks: got %#v want %#v", candidate.ClaudeHaikuFallbackModels, want)
	}
}

func TestConfigModal_ArrayAltDownReordersRows(t *testing.T) {
	modal := NewConfigModal()
	base := config.DefaultSettings()
	base.ClaudeHaikuFallbackModels = []string{"gpt-5-mini", "grok-code-fast-1"}
	base.ClaudeHaikuFallbackModelsUI = []config.HaikuFallbackModel{
		{Model: "gpt-5-mini"},
		{Model: "grok-code-fast-1"},
	}
	if err := modal.Open(&base); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	focusFieldByKey(t, modal, "claude_haiku_fallback_models_ui")
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyDown, Alt: true})

	rows := modal.form.ObjectArrayValues["claude_haiku_fallback_models_ui"]
	if got := rows[0]["model"]; got != "grok-code-fast-1" {
		t.Fatalf("expected alt+down to move first row down, got %#v", rows)
	}
	if got := rows[1]["model"]; got != "gpt-5-mini" {
		t.Fatalf("expected alt+down to move second row up, got %#v", rows)
	}
}

func TestConfigModal_InputWidthIsAdaptiveNotFixed(t *testing.T) {
	short := newModalTextInput("a", "")
	long := newModalTextInput(strings.Repeat("x", 256), "")
	if short.Width >= long.Width {
		t.Fatalf("expected long input to have wider box: short=%d long=%d", short.Width, long.Width)
	}
	if long.Width != adaptiveInputWidthMax {
		t.Fatalf("expected long width capped at %d, got %d", adaptiveInputWidthMax, long.Width)
	}

	modal := NewConfigModal()
	settings := config.DefaultSettings()
	if err := modal.Open(&settings); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	focusFieldByKey(t, modal, "retry_backoff")
	before := modal.scalarInputs["retry_backoff"].Width
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", 32))})
	after := modal.scalarInputs["retry_backoff"].Width
	if after <= before {
		t.Fatalf("expected width to expand after typing: before=%d after=%d", before, after)
	}
}

func focusFieldByKey(t *testing.T, modal *ConfigModal, key string) {
	t.Helper()
	if modal == nil {
		t.Fatalf("modal is nil")
	}
	_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyHome})
	for i := 0; i < 128; i++ {
		if modal.CurrentFieldKey() == key {
			return
		}
		_ = modal.HandleKey(tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("failed to focus field %q, current=%q", key, modal.CurrentFieldKey())
}

func stripANSI(input string) string {
	return ansiCodePattern.ReplaceAllString(input, "")
}

func isArrayRowEmpty(row map[string]string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

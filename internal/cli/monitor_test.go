package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"copilot-proxy/internal/auth"
	"copilot-proxy/internal/cli/tui"
	"copilot-proxy/internal/config"
	"copilot-proxy/internal/monitor"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewMonitorModel(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{
		Collector:  collector,
		Models:     []monitor.ModelInfo{{ID: "gpt-4o", Name: "GPT-4o", Endpoints: []string{"/chat/completions"}}},
		UserInfo:   &monitor.UserInfo{Plan: "business", Organization: "TestOrg"},
		AuthConfig: &config.AuthConfig{Default: "user1", Accounts: []config.Account{{User: "user1"}}},
	}

	model := NewMonitorModel(&deps, "127.0.0.1:4000")

	if model.state != tui.ViewStats {
		t.Errorf("expected initial state tui.ViewStats, got %d", model.state)
	}
	if model.serverAddr != "127.0.0.1:4000" {
		t.Errorf("expected serverAddr 127.0.0.1:4000, got %s", model.serverAddr)
	}
}

func TestMonitorModel_ViewSwitching(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	// View order: Stats(0), Activity(1), Models(2), Logs(3)
	// Keys: 1=Stats, 2=Activity, 3=Models, 4=Logs
	tests := []struct {
		key      string
		expected tui.ViewState
	}{
		{"1", tui.ViewStats},
		{"2", tui.ViewActivity},
		{"3", tui.ViewModels},
		{"4", tui.ViewLogs},
		{"1", tui.ViewStats},
	}

	for _, tc := range tests {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)}
		updated, _ := model.Update(msg)
		updatedModel, ok := updated.(*MonitorModel)
		if !ok {
			t.Fatalf("expected MonitorModel, got %T", updated)
		}
		model = *updatedModel
		if model.state != tc.expected {
			t.Errorf("after pressing %s, expected state %d, got %d", tc.key, tc.expected, model.state)
		}
	}
}

func TestMonitorModel_ArrowKeyNavigation(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	// Start at tui.ViewStats
	if model.state != tui.ViewStats {
		t.Fatal("expected initial state tui.ViewStats")
	}

	// Right arrow should go to tui.ViewActivity (next in order: Stats -> Activity)
	msg := tea.KeyMsg{Type: tea.KeyRight}
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.state != tui.ViewActivity {
		t.Errorf("expected tui.ViewActivity after right arrow, got %d", model.state)
	}

	// Left arrow should go back to tui.ViewStats
	msg = tea.KeyMsg{Type: tea.KeyLeft}
	updated, _ = model.Update(msg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.state != tui.ViewStats {
		t.Errorf("expected tui.ViewStats after left arrow, got %d", model.state)
	}

	// Left arrow from tui.ViewStats should wrap to tui.ViewLogs
	msg = tea.KeyMsg{Type: tea.KeyLeft}
	updated, _ = model.Update(msg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.state != tui.ViewLogs {
		t.Errorf("expected tui.ViewLogs after left arrow wrap, got %d", model.state)
	}

	// Right arrow from tui.ViewLogs should wrap to tui.ViewStats
	msg = tea.KeyMsg{Type: tea.KeyRight}
	updated, _ = model.Update(msg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.state != tui.ViewStats {
		t.Errorf("expected tui.ViewStats after right arrow wrap, got %d", model.state)
	}
}

func TestMonitorModel_QuitKey(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	updated, cmd := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if !model.quitting {
		t.Error("expected quitting to be true after pressing ctrl+c")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestMonitorModel_TickUpdatesSnapshot(t *testing.T) {
	collector := monitor.NewCollector(100)
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	// Initial snapshot should be empty
	if model.snapshot.TotalRequests != 0 {
		t.Errorf("expected empty initial snapshot, got %d requests", model.snapshot.TotalRequests)
	}

	// Tick should update snapshot
	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.snapshot.TotalRequests != 1 {
		t.Errorf("expected 1 request after tick, got %d", model.snapshot.TotalRequests)
	}
}

func TestMonitorModel_TickUpdatesSnapshotInActivity(t *testing.T) {
	collector := monitor.NewCollector(100)
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.state = tui.ViewActivity

	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.snapshot.TotalRequests != 1 {
		t.Errorf("expected 1 request after tick in activity view, got %d", model.snapshot.TotalRequests)
	}
}

func TestMonitorModel_ClearLogsResetsOffset(t *testing.T) {
	collector := monitor.NewCollector(100)
	for range 50 {
		collector.RecordLocal(&monitor.RequestRecord{
			Timestamp:  time.Now(),
			Model:      "gpt-4o",
			StatusCode: 200,
		})
	}

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.height = 20
	model.state = tui.ViewLogs

	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	// Scroll down to create offset
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	updated, _ = model.Update(downMsg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	// Clear logs should reset offset
	clearMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	updated, _ = model.Update(clearMsg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.logsView.VisibleLines() <= 0 {
		t.Errorf("expected visible lines to be positive")
	}
}

func TestMonitorModel_ClearStatsResetsCountersOnly(t *testing.T) {
	collector := monitor.NewCollector(100)
	now := time.Now()
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:  now,
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   100 * time.Millisecond,
		IsAgent:    false,
	})
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:  now,
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   120 * time.Millisecond,
		IsAgent:    true,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tickMsg(time.Now()))
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.snapshot.TotalRequests != 1 {
		t.Fatalf("expected one user request before clear, got %d", model.snapshot.TotalRequests)
	}
	if len(model.snapshot.ByModel) != 1 {
		t.Fatalf("expected one model bucket before clear, got %d", len(model.snapshot.ByModel))
	}
	if len(model.snapshot.ActivityHour) == 0 {
		t.Fatalf("expected activity counters before clear")
	}

	clearMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	updated, _ = model.Update(clearMsg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.snapshot.TotalRequests != 0 {
		t.Fatalf("expected user counters to be cleared, got %d", model.snapshot.TotalRequests)
	}
	if len(model.snapshot.ByModel) != 0 {
		t.Fatalf("expected model counters to be cleared, got %d entries", len(model.snapshot.ByModel))
	}
	if len(model.snapshot.RecentRequests) != 2 {
		t.Fatalf("expected request logs to be retained, got %d", len(model.snapshot.RecentRequests))
	}
	if len(model.snapshot.ActivityHour) == 0 {
		t.Fatalf("expected activity counters to remain after stats clear")
	}
}

func buildLogsMonitorModelForMouseTests(t *testing.T) MonitorModel {
	t.Helper()

	collector := monitor.NewCollector(100)
	now := time.Now()
	for i := range 20 {
		collector.RecordLocal(&monitor.RequestRecord{
			Timestamp:  now.Add(-time.Duration(i) * time.Second),
			Method:     "POST",
			Path:       "/v1/chat/completions",
			Model:      "model-" + string(rune('A'+i)),
			StatusCode: 200,
			Duration:   10 * time.Millisecond,
		})
	}

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.state = tui.ViewLogs

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, _ = model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)

	return model
}

func mustMonitorModelFromUpdate(t *testing.T, updated tea.Model) *MonitorModel {
	t.Helper()
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	return updatedModel
}

func TestMonitorModel_LogsViewMouseWheelScrollWithCtrl(t *testing.T) {
	model := buildLogsMonitorModelForMouseTests(t)

	before := model.View()
	if !strings.Contains(before, "model-A") {
		t.Fatalf("expected initial logs to contain newest model-A")
	}

	updated, _ := model.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Ctrl:   true,
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	after := model.View()
	if strings.Contains(after, "model-A") {
		t.Fatalf("expected model-A to scroll out after ctrl+wheel down")
	}
}

func TestMonitorModel_LogsViewMouseWheelWithoutCtrlIgnored(t *testing.T) {
	model := buildLogsMonitorModelForMouseTests(t)

	before := model.View()
	if !strings.Contains(before, "model-A") {
		t.Fatalf("expected initial logs to contain newest model-A")
	}

	updated, _ := model.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	after := model.View()
	if !strings.Contains(after, "model-A") {
		t.Fatalf("expected wheel down without ctrl to be ignored")
	}
}

func TestMonitorModel_LogsViewMouseWheelRemainsCtrlGatedAfterBlurFocus(t *testing.T) {
	model := buildLogsMonitorModelForMouseTests(t)

	updated, _ := model.Update(tea.BlurMsg{})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, _ = model.Update(tea.FocusMsg{})
	model = *mustMonitorModelFromUpdate(t, updated)

	withoutCtrlBefore := model.View()
	updated, _ = model.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	withoutCtrlAfter := model.View()
	if withoutCtrlBefore != withoutCtrlAfter {
		t.Fatalf("expected wheel without ctrl to remain ignored after blur/focus")
	}

	updated, _ = model.Update(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Ctrl:   true,
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	withCtrlAfter := model.View()
	if withCtrlAfter == withoutCtrlAfter {
		t.Fatalf("expected ctrl+wheel down to scroll after blur/focus")
	}
}

func TestMonitorModel_InitLoadsUserInfo(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Init")
	}
	if _, ok := cmd().(tea.BatchMsg); !ok {
		t.Fatalf("expected BatchMsg, got %T", cmd())
	}
}

func TestMonitorModel_WindowResize(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.width != 100 || model.height != 50 {
		t.Errorf("expected size 100x50, got %dx%d", model.width, model.height)
	}
}

func TestMonitorModel_ViewRendering(t *testing.T) {
	collector := monitor.NewCollector(100)
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:  time.Now(),
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{
		Collector:  collector,
		Models:     []monitor.ModelInfo{{ID: "gpt-4o", Name: "GPT-4o", Endpoints: []string{"/chat/completions"}}},
		UserInfo:   &monitor.UserInfo{Plan: "business", Organization: "TestOrg"},
		AuthConfig: &config.AuthConfig{Default: "user1", Accounts: []config.Account{{User: "user1"}}},
	}
	model := NewMonitorModel(&deps, "127.0.0.1:4000")
	model.width = 80
	model.height = 40

	// Update snapshot
	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	// Test each view renders without panic and contains header tabs
	views := []tui.ViewState{tui.ViewStats, tui.ViewModels, tui.ViewActivity, tui.ViewLogs}
	for _, view := range views {
		model.state = view
		output := model.View()
		if output == "" {
			t.Errorf("view %d rendered empty output", view)
		}
		// All views should contain header tabs
		if !strings.Contains(output, "1:Stats") {
			t.Errorf("view %d missing header tabs (1:Stats)", view)
		}
		if !strings.Contains(output, "3:Models") {
			t.Errorf("view %d missing header tabs (3:Models)", view)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 2, "ab"},
		{"abc", 2, "ab"},
	}

	for _, tc := range tests {
		result := tui.Truncate(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Microsecond, "<1ms"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.5s"},
		{2 * time.Second, "2.0s"},
	}

	for _, tc := range tests {
		result := tui.FormatDuration(tc.input)
		if result != tc.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestHeatmapCell(t *testing.T) {
	tests := []struct {
		count    int
		maxCount int
	}{
		{0, 100},
		{5, 100},
		{15, 100},
		{30, 100},
		{75, 100},
	}

	for _, tc := range tests {
		result := tui.HeatmapCell(tc.count, tc.maxCount)
		if result == "" {
			t.Errorf("HeatmapCell(%d, %d) returned empty string", tc.count, tc.maxCount)
		}
	}
}

func TestFormatEndpoints(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{"/chat/completions"}, "C"},
		{[]string{"/responses"}, "R"},
		{[]string{"/v1/messages"}, "M"},
		{[]string{"/chat/completions", "/responses"}, "C R"},
		{[]string{"/chat/completions", "/responses", "/v1/messages"}, "C M R"},
		{[]string{"/responses", "/v1/messages", "/chat/completions"}, "C M R"},
		{[]string{}, ""},
		{[]string{"/unknown"}, "/unknown"},
		{[]string{"/very/long/endpoint/path"}, "/very/long"},
	}

	for _, tc := range tests {
		result := tui.FormatEndpoints(tc.input)
		if result != tc.expected {
			t.Errorf("FormatEndpoints(%v) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestFormatContextWindow(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "-"},
		{-1, "-"},
		{16384, "16K"},
		{32768, "32K"},
		{128000, "128K"},
		{264000, "264K"},
		{400000, "400K"},
		{1000000, "1.0M"},
		{2000000, "2.0M"},
	}

	for _, tc := range tests {
		result := tui.FormatContextWindow(tc.input)
		if result != tc.expected {
			t.Errorf("FormatContextWindow(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestFormatPromptOutputContext(t *testing.T) {
	tests := []struct {
		prompt   int
		output   int
		expected string
	}{
		{0, 0, "-"},
		{-1, -1, "-"},
		{128000, 16000, "↑ 128K ↓ 16K"},
		{1000000, 2000000, "↑ 1.0M ↓ 2.0M"},
		{500, 0, "↑ 0K ↓ -"},
	}

	for _, tc := range tests {
		result := tui.FormatPromptOutputContext(tc.prompt, tc.output)
		if result != tc.expected {
			t.Errorf("FormatPromptOutputContext(%d,%d) = %q, want %q", tc.prompt, tc.output, result, tc.expected)
		}
	}
}

func TestMonitorModel_DebugMode(t *testing.T) {
	collector := monitor.NewCollector(100)
	debugLogger := monitor.NewDebugLogger()
	t.Cleanup(func() {
		_ = debugLogger.Close()
	})
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	if err := debugLogger.Init(tmpDir); err != nil {
		t.Fatalf("init debug logger: %v", err)
	}
	auth := &config.AuthConfig{}
	deps := MonitorDeps{
		Collector:   collector,
		DebugLogger: debugLogger,
		AuthConfig:  auth,
	}
	model := NewMonitorModel(&deps, "")

	// Switch to logs view (debug toggle only works in Logs view)
	model.state = tui.ViewLogs

	// Toggle debug mode with 'd' key
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
	_, _ = model.Update(keyMsg)

	// Verify debug logging is enabled
	if !debugLogger.DebugEnabled() {
		t.Error("debug logger should be enabled")
	}

	// Toggle again
	_, _ = model.Update(keyMsg)

	// Verify debug logging is disabled
	if debugLogger.DebugEnabled() {
		t.Error("debug logger should be disabled")
	}
}

func TestMonitorModel_LogsViewRendering(t *testing.T) {
	collector := monitor.NewCollector(100)
	collector.RecordLocal(&monitor.RequestRecord{
		Timestamp:    time.Now(),
		Method:       "POST",
		Path:         "/v1/chat/completions",
		UpstreamPath: "/chat/completions",
		Model:        "gpt-4o",
		StatusCode:   200,
		Duration:     500 * time.Millisecond,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.width = 80
	model.height = 40

	// Update snapshot
	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	// Switch to logs view
	model.state = tui.ViewLogs

	// Render logs view
	view := model.View()
	if view == "" {
		t.Error("logs view should not be empty")
	}

	// Check that logs are shown
	if !strings.Contains(view, "Timestamp") || !strings.Contains(view, "Model") {
		t.Error("logs view should contain table header")
	}
	if !strings.Contains(view, "Duration") || !strings.Contains(view, "Stream") {
		t.Error("logs view should contain Duration/Stream headers")
	}
	if !strings.Contains(view, "gpt-4o") {
		t.Error("logs view should contain model name")
	}
	if !strings.Contains(view, "POST /chat/completions") {
		t.Error("logs view should render request using upstream path")
	}
	if strings.Contains(view, "POST /v1/chat/completions") {
		t.Error("logs view should not render request using local path when upstream path is available")
	}
}

func TestMonitorModel_ConfigModalOpenAndClose(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{
		Collector: collector,
		LoadSettings: func() (config.Settings, error) {
			return config.DefaultSettings(), nil
		},
		ApplySettings: func(settings config.Settings) (config.Settings, error) {
			return settings, nil
		},
	}
	model := NewMonitorModel(&deps, "127.0.0.1:4000")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.configModal == nil || !model.configModal.IsOpen() {
		t.Fatalf("expected config modal to be open")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.configModal != nil && model.configModal.IsOpen() {
		t.Fatalf("expected config modal to be closed after esc")
	}
}

func TestMonitorModel_ConfigModalSaveAppliesSettings(t *testing.T) {
	collector := monitor.NewCollector(100)
	applied := false
	deps := MonitorDeps{
		Collector: collector,
		LoadSettings: func() (config.Settings, error) {
			return config.DefaultSettings(), nil
		},
		ApplySettings: func(settings config.Settings) (config.Settings, error) {
			applied = true
			settings.ListenAddr = "127.0.0.1:5111"
			return settings, nil
		},
	}
	model := NewMonitorModel(&deps, "127.0.0.1:4000")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if cmd == nil {
		t.Fatalf("expected save command")
	}

	msg := cmd()
	updated, _ = model.Update(msg)
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if !applied {
		t.Fatalf("expected apply settings callback to be called")
	}
	if model.serverAddr != "127.0.0.1:5111" {
		t.Fatalf("expected serverAddr to update, got %q", model.serverAddr)
	}
	if model.configModal != nil && model.configModal.IsOpen() {
		t.Fatalf("expected modal to close after successful save")
	}
}

func TestMonitorModel_AccountModalOpenOnlyInStats(t *testing.T) {
	collector := monitor.NewCollector(100)
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1"},
			{User: "u2"},
		},
	}
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: auth,
	}
	model := NewMonitorModel(&deps, "")

	model.state = tui.ViewModels
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.accountModal != nil && model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to stay closed outside stats view")
	}

	model.state = tui.ViewStats
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.accountModal == nil || !model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to open in stats view")
	}
}

func TestMonitorModel_AccountModalActivateSuccessRefreshes(t *testing.T) {
	collector := monitor.NewCollector(100)
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "ghu-1"},
			{User: "u2", GhToken: "ghu-2"},
		},
	}
	activatedUser := ""
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: auth,
		UserInfo:   &monitor.UserInfo{Plan: "business"},
		ActivateAccount: func(user string) error {
			activatedUser = user
			auth.Default = user
			return nil
		},
	}
	model := NewMonitorModel(&deps, "")
	model.loadedUserInfo = true
	model.userInfo = &monitor.UserInfo{Plan: "business"}
	model.sharedState.UserInfo = model.userInfo

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if activatedUser != "u2" {
		t.Fatalf("expected activate callback for u2, got %q", activatedUser)
	}
	if model.accountModal != nil && model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to close after successful activation")
	}
	if cmd == nil {
		t.Fatalf("expected refresh command after successful activation")
	}
	if model.sharedState.UserInfo != nil {
		t.Fatalf("expected shared user info to be cleared before refresh")
	}
	if model.loadedUserInfo {
		t.Fatalf("expected loadedUserInfo to reset before refresh")
	}
}

func TestMonitorModel_AccountModalActivateFailureKeepsModalOpen(t *testing.T) {
	collector := monitor.NewCollector(100)
	auth := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1"},
			{User: "u2"},
		},
	}
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: auth,
		ActivateAccount: func(user string) error {
			return errors.New("activate failed")
		},
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, ok = updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if cmd != nil {
		t.Fatalf("expected no refresh command when activation fails")
	}
	if model.accountModal == nil || !model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to remain open when activation fails")
	}
	if !strings.Contains(model.accountModal.View(), "activate failed") {
		t.Fatalf("expected modal to show activation error")
	}
}

func TestMonitorModel_AccountModalOpenWithoutAccountsShowsAddRow(t *testing.T) {
	collector := monitor.NewCollector(100)
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: &config.AuthConfig{},
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.accountModal == nil || !model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to open even when no accounts configured")
	}
	if !strings.Contains(model.accountModal.View(), "Add Account") {
		t.Fatalf("expected Add Account row in modal view, got:\n%s", model.accountModal.View())
	}
}

func TestMonitorModel_AddAccountSuccessKeepsDefaultAndRefreshesModalList(t *testing.T) {
	collector := monitor.NewCollector(100)
	authCfg := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "t1"},
		},
	}
	var added config.Account
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: authCfg,
		AddAccount: func(account config.Account) error {
			added = account
			return nil
		},
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown}) // select Add Account row
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected start add-account command")
	}
	seq := model.accountAuthSeq

	updated, cmd = model.Update(accountDeviceCodeMsg{
		seq: seq,
		device: auth.DeviceCodeResponse{
			VerificationURI: "https://github.com/login/device",
			UserCode:        "ABCD-EFGH",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected poll token command")
	}

	updated, cmd = model.Update(accountTokenMsg{
		seq:   seq,
		token: "ghu-new",
	})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected fetch user command")
	}

	updated, cmd = model.Update(accountUserMsg{
		seq:   seq,
		login: "u2",
		token: "ghu-new",
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd != nil {
		t.Fatalf("expected no user refresh command when accounts already existed")
	}
	if added.User != "u2" || added.GhToken != "ghu-new" {
		t.Fatalf("expected add callback with new account, got %+v", added)
	}
	if authCfg.Default != "u1" {
		t.Fatalf("expected default account to remain u1, got %q", authCfg.Default)
	}
	if len(authCfg.Accounts) != 2 {
		t.Fatalf("expected account list to include new account, got %d", len(authCfg.Accounts))
	}
	if model.accountModal == nil || !model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to stay open after add success")
	}
	if !strings.Contains(model.accountModal.View(), "u2") {
		t.Fatalf("expected modal to show new account, got:\n%s", model.accountModal.View())
	}
}

func TestMonitorModel_AddAccountFirstAccountTriggersUserRefresh(t *testing.T) {
	collector := monitor.NewCollector(100)
	authCfg := &config.AuthConfig{}
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: authCfg,
		AddAccount: func(account config.Account) error {
			return nil
		},
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Add row is selected by default
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected start add-account command")
	}
	seq := model.accountAuthSeq

	updated, _ = model.Update(accountDeviceCodeMsg{
		seq: seq,
		device: auth.DeviceCodeResponse{
			VerificationURI: "https://github.com/login/device",
			UserCode:        "ABCD-EFGH",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, _ = model.Update(accountTokenMsg{
		seq:   seq,
		token: "ghu-first",
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd = model.Update(accountUserMsg{
		seq:   seq,
		login: "u-first",
		token: "ghu-first",
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected user-info refresh command when first account is added")
	}
	if authCfg.Default != "u-first" {
		t.Fatalf("expected first added account to become default, got %q", authCfg.Default)
	}
	if len(authCfg.Accounts) != 1 {
		t.Fatalf("expected one account after first add, got %d", len(authCfg.Accounts))
	}
}

func TestMonitorModel_AddAccountCancelIgnoresLateMessages(t *testing.T) {
	collector := monitor.NewCollector(100)
	authCfg := &config.AuthConfig{
		Default: "u1",
		Accounts: []config.Account{
			{User: "u1", GhToken: "t1"},
		},
	}
	addCalled := false
	deps := MonitorDeps{
		Collector:  collector,
		AuthConfig: authCfg,
		AddAccount: func(account config.Account) error {
			addCalled = true
			return nil
		},
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.accountAuthSeq

	updated, _ = model.Update(accountDeviceCodeMsg{
		seq: seq,
		device: auth.DeviceCodeResponse{
			VerificationURI: "https://github.com/login/device",
			UserCode:        "ABCD-EFGH",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = *mustMonitorModelFromUpdate(t, updated)
	if !strings.Contains(model.statusMsg, "canceled") {
		t.Fatalf("expected canceled status, got %q", model.statusMsg)
	}

	updated, _ = model.Update(accountUserMsg{
		seq:   seq,
		login: "late-user",
		token: "late-token",
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if addCalled {
		t.Fatalf("expected late message to be ignored after cancel")
	}
	if len(authCfg.Accounts) != 1 || authCfg.Accounts[0].User != "u1" {
		t.Fatalf("expected auth config unchanged after cancel, got %+v", authCfg.Accounts)
	}
}

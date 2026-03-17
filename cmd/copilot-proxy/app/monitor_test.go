package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"copilot-proxy/cmd/copilot-proxy/app/tui"
	"copilot-proxy/internal/config"
	core "copilot-proxy/internal/core"
	"copilot-proxy/internal/core/account"
	"copilot-proxy/internal/core/observability"
	"copilot-proxy/internal/core/stats"
	"copilot-proxy/internal/models"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewMonitorModel(t *testing.T) {
	collector := observability.NewCollector(100)
	deps := MonitorDeps{
		Collector:  collector,
		Models:     []models.ModelInfo{{ID: "gpt-4o", Name: "GPT-4o", Endpoints: []string{"/chat/completions"}}},
		UserInfo:   &core.UserInfo{Plan: "business", Organization: "TestOrg"},
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
	collector := observability.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	// View order: Stats(0), Models(1), Logs(2)
	// Keys: 1=Stats, 2=Models, 3=Logs
	tests := []struct {
		key      string
		expected tui.ViewState
	}{
		{"1", tui.ViewStats},
		{"2", tui.ViewModels},
		{"3", tui.ViewLogs},
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
	collector := observability.NewCollector(100)
	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")

	// Start at tui.ViewStats
	if model.state != tui.ViewStats {
		t.Fatal("expected initial state tui.ViewStats")
	}

	// Right arrow should go to tui.ViewModels (next in order: Stats -> Models)
	msg := tea.KeyMsg{Type: tea.KeyRight}
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel
	if model.state != tui.ViewModels {
		t.Errorf("expected tui.ViewModels after right arrow, got %d", model.state)
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
	collector := observability.NewCollector(100)
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
	collector := observability.NewCollector(100)
	collector.RecordLocal(&core.RequestRecord{
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

func TestMonitorModel_TickUpdatesSnapshotInLogs(t *testing.T) {
	collector := observability.NewCollector(100)
	collector.RecordLocal(&core.RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	model.state = tui.ViewLogs

	msg := tickMsg(time.Now())
	updated, _ := model.Update(msg)
	updatedModel, ok := updated.(*MonitorModel)
	if !ok {
		t.Fatalf("expected MonitorModel, got %T", updated)
	}
	model = *updatedModel

	if model.snapshot.TotalRequests != 1 {
		t.Errorf("expected 1 request after tick in logs view, got %d", model.snapshot.TotalRequests)
	}
}

func TestMonitorModel_TickTogglesLogsBlinkAndKeepsSnapshotRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	collector.RecordLocal(&core.RequestRecord{
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "")
	if !model.sharedState.LogsBlinkOn {
		t.Fatalf("expected LogsBlinkOn default to true")
	}

	updated, _ := model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)
	if model.sharedState.LogsBlinkOn {
		t.Fatalf("expected first tick to toggle LogsBlinkOn to false")
	}
	if model.snapshot.TotalRequests != 1 {
		t.Fatalf("expected snapshot refresh to remain intact after first tick, got %d", model.snapshot.TotalRequests)
	}

	updated, _ = model.Update(tickMsg(time.Now().Add(time.Second)))
	model = *mustMonitorModelFromUpdate(t, updated)
	if !model.sharedState.LogsBlinkOn {
		t.Fatalf("expected second tick to toggle LogsBlinkOn back to true")
	}
	if model.snapshot.TotalRequests != 1 {
		t.Fatalf("expected snapshot refresh to remain intact after second tick, got %d", model.snapshot.TotalRequests)
	}
}

func TestMonitorModel_ClearLogsResetsOffset(t *testing.T) {
	collector := observability.NewCollector(100)
	for range 50 {
		collector.RecordLocal(&core.RequestRecord{
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
	collector := observability.NewCollector(100)
	now := time.Now()
	collector.RecordLocal(&core.RequestRecord{
		Timestamp:  now,
		Model:      "gpt-4o",
		StatusCode: 200,
		Duration:   100 * time.Millisecond,
		IsAgent:    false,
	})
	collector.RecordLocal(&core.RequestRecord{
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
}

func buildLogsMonitorModelForMouseTests(t *testing.T) MonitorModel {
	t.Helper()

	collector := observability.NewCollector(100)
	now := time.Now()
	for i := range 20 {
		collector.RecordLocal(&core.RequestRecord{
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
	collector := observability.NewCollector(100)
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

func TestMonitorModel_InitQueuesUserInfoRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")

	cmd := model.Init()
	if cmd == nil {
		t.Fatalf("expected init command")
	}
	msg := cmd()
	if _, ok := msg.(tea.BatchMsg); !ok {
		t.Fatalf("expected BatchMsg from init, got %T", msg)
	}
	state := model.userInfoQueue.State()
	if !state.Pending {
		t.Fatalf("expected init to queue user info refresh")
	}
	if !state.Armed {
		t.Fatalf("expected init to arm debounce timer")
	}
	if state.InFlight {
		t.Fatalf("expected init queue to be waiting, not in-flight")
	}
}

func TestMonitorModel_ManualRefreshQueuesUserInfoRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")
	model.state = tui.ViewStats

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected manual refresh to enqueue debounce command")
	}
	state := model.userInfoQueue.State()
	if !state.Pending || !state.Armed {
		t.Fatalf("expected manual refresh to queue and arm debounce")
	}
	if state.InFlight {
		t.Fatalf("expected manual refresh to wait for due trigger before calling API")
	}
	if !strings.Contains(model.statusMsg, "Queued user info refresh (3s)") {
		t.Fatalf("expected queue status message, got %q", model.statusMsg)
	}
}

func TestMonitorModel_AgentPremiumEventQueuesRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	deps := MonitorDeps{
		Collector: collector,
		Models: []models.ModelInfo{
			{ID: "gpt-4o", IsPremium: true},
		},
	}
	model := NewMonitorModel(&deps, "")

	collector.RecordLocal(&core.RequestRecord{
		RequestID:  "req-agent-premium-1",
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsAgent:    true,
	})

	updated, _ := model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)

	state := model.userInfoQueue.State()
	if !state.Pending || !state.Armed {
		t.Fatalf("expected agent premium event to enqueue user info refresh")
	}
}

func TestMonitorModel_FirstTriggerDebounceDoesNotResetOnBurst(t *testing.T) {
	collector := observability.NewCollector(100)
	deps := MonitorDeps{
		Collector: collector,
		Models: []models.ModelInfo{
			{ID: "gpt-4o", IsPremium: true},
		},
	}
	model := NewMonitorModel(&deps, "")

	collector.RecordLocal(&core.RequestRecord{
		RequestID:  "req-agent-premium-1",
		Timestamp:  time.Now(),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsAgent:    true,
	})
	updated, _ := model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)
	firstState := model.userInfoQueue.State()
	firstSeq := firstState.Seq
	if firstSeq == 0 {
		t.Fatalf("expected first burst event to arm debounce sequence")
	}

	collector.RecordLocal(&core.RequestRecord{
		RequestID:  "req-agent-premium-2",
		Timestamp:  time.Now().Add(time.Second),
		Model:      "gpt-4o",
		StatusCode: 200,
		IsAgent:    true,
	})
	updated, _ = model.Update(tickMsg(time.Now().Add(time.Second)))
	model = *mustMonitorModelFromUpdate(t, updated)

	state := model.userInfoQueue.State()
	if state.Seq != firstSeq {
		t.Fatalf("expected first-trigger debounce to keep seq=%d, got %d", firstSeq, state.Seq)
	}
	if !state.Pending || !state.Armed {
		t.Fatalf("expected queue to remain pending/armed during burst")
	}
}

func TestMonitorModel_DueMsgStartsUserInfoRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.userInfoQueue.State().Seq

	updated, cmd := model.Update(userInfoRefreshDueMsg{seq: seq})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected due message to start user info API refresh")
	}
	state := model.userInfoQueue.State()
	if !state.InFlight {
		t.Fatalf("expected queue inFlight=true after due trigger")
	}
	if state.Armed {
		t.Fatalf("expected queue armed=false after consuming due trigger")
	}
}

func TestMonitorModel_UserInfoLoadFailureClearsQueue(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.userInfoQueue.State().Seq

	updated, _ = model.Update(userInfoRefreshDueMsg{seq: seq})
	model = *mustMonitorModelFromUpdate(t, updated)
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected in-flight refresh before load completion")
	}

	updated, _ = model.Update(userInfoLoadedMsg{err: errors.New("fetch failed")})
	model = *mustMonitorModelFromUpdate(t, updated)

	state := model.userInfoQueue.State()
	if state.Pending {
		t.Fatalf("expected queue pending=false after failed refresh completion")
	}
	if state.Armed {
		t.Fatalf("expected queue armed=false after failed refresh completion")
	}
	if state.InFlight {
		t.Fatalf("expected queue inFlight=false after failed refresh completion")
	}
}

func TestMonitorModel_ManualRefreshDuringInFlightDefersFollowUp(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.userInfoQueue.State().Seq

	updated, _ = model.Update(userInfoRefreshDueMsg{seq: seq})
	model = *mustMonitorModelFromUpdate(t, updated)
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected first refresh to be in-flight")
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd != nil {
		t.Fatalf("expected no immediate API call when deferring to post in-flight")
	}
	if !model.userInfoRefreshAfterInFlight {
		t.Fatalf("expected manual refresh to defer follow-up while in-flight")
	}
	if strings.Contains(model.statusMsg, "Queued user info refresh (3s)") {
		t.Fatalf("did not expect 3s queue status while in-flight, got %q", model.statusMsg)
	}
	if !strings.Contains(model.statusMsg, "after current request") {
		t.Fatalf("expected in-flight queue status, got %q", model.statusMsg)
	}

	updated, cmd = model.Update(userInfoLoadedMsg{
		info: &core.UserInfo{Plan: "business", Organization: "old"},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected deferred refresh to start after current request completes")
	}
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected deferred refresh to become in-flight")
	}
}

func TestMonitorModel_ViewEnterDuringInFlightDoesNotDeferRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.userInfoQueue.State().Seq

	updated, _ = model.Update(userInfoRefreshDueMsg{seq: seq})
	model = *mustMonitorModelFromUpdate(t, updated)
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected first refresh to be in-flight")
	}

	model.state = tui.ViewModels
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd != nil {
		t.Fatalf("expected no immediate cmd when entering stats during in-flight refresh")
	}
	if model.userInfoRefreshAfterInFlight {
		t.Fatalf("expected view-enter refresh not to mark deferred follow-up")
	}

	updated, cmd = model.Update(userInfoLoadedMsg{
		info: &core.UserInfo{Plan: "business", Organization: "current"},
	})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd != nil {
		t.Fatalf("expected no follow-up refresh for passive view-enter attempt")
	}
	if model.userInfoQueue.State().InFlight {
		t.Fatalf("expected in-flight flag to clear after completion")
	}
	if model.sharedState.UserInfo == nil {
		t.Fatalf("expected user info to be updated from completed in-flight refresh")
	}
}

func TestMonitorModel_WindowResize(t *testing.T) {
	collector := observability.NewCollector(100)
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
	collector := observability.NewCollector(100)
	collector.RecordLocal(&core.RequestRecord{
		Timestamp:  time.Now(),
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		StatusCode: 200,
	})

	deps := MonitorDeps{
		Collector:  collector,
		Models:     []models.ModelInfo{{ID: "gpt-4o", Name: "GPT-4o", Endpoints: []string{"/chat/completions"}}},
		UserInfo:   &core.UserInfo{Plan: "business", Organization: "TestOrg"},
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
	views := []tui.ViewState{tui.ViewStats, tui.ViewModels, tui.ViewLogs}
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
		if !strings.Contains(output, "2:Models") {
			t.Errorf("view %d missing header tabs (2:Models)", view)
		}
		if !strings.Contains(output, "3:Logs") {
			t.Errorf("view %d missing header tabs (3:Logs)", view)
		}
	}
}

func TestMonitorModel_LogsViewRenderingFitsWindowAndKeepsHeader(t *testing.T) {
	collector := observability.NewCollector(200)
	now := time.Now()
	for i := range 120 {
		collector.RecordLocal(&core.RequestRecord{
			Timestamp:    now.Add(-time.Duration(i) * time.Second),
			Method:       "POST",
			Path:         "/v1/chat/completions",
			UpstreamPath: "/chat/completions",
			Model:        "gpt-4o",
			StatusCode:   200,
		})
	}

	deps := MonitorDeps{Collector: collector}
	model := NewMonitorModel(&deps, "127.0.0.1:4000")
	model.state = tui.ViewLogs

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)

	output := model.View()
	if !strings.Contains(output, "Copilot Proxy") {
		t.Fatalf("expected header title in output")
	}
	if !strings.Contains(output, "1:Stats") || !strings.Contains(output, "2:Models") || !strings.Contains(output, "3:Logs") {
		t.Fatalf("expected all header tabs to remain visible in logs output")
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) > model.height {
		t.Fatalf("expected rendered lines <= window height (%d), got %d", model.height, len(lines))
	}
}

func TestMonitorModel_LogsViewSmallWindowRendersAndPages(t *testing.T) {
	collector := observability.NewCollector(50)
	now := time.Now()
	for i := range 12 {
		collector.RecordLocal(&core.RequestRecord{
			Timestamp:    now.Add(-time.Duration(i) * time.Second),
			Method:       "POST",
			Path:         "/v1/chat/completions",
			UpstreamPath: "/chat/completions",
			Model:        fmt.Sprintf("model-%02d", i),
			StatusCode:   200,
			Duration:     50 * time.Millisecond,
		})
	}

	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "127.0.0.1:4000")
	model.state = tui.ViewLogs

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 6})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tickMsg(time.Now()))
	model = *mustMonitorModelFromUpdate(t, updated)

	before := model.View()
	if !strings.Contains(before, "1:Stats") || !strings.Contains(before, "2:Models") || !strings.Contains(before, "3:Logs") {
		t.Fatalf("expected all header tabs in small-window logs output")
	}
	if !strings.Contains(before, "model-00") {
		t.Fatalf("expected first logs page to show newest row in small window")
	}
	if strings.Contains(before, "model-01") {
		t.Fatalf("expected first small-window page to show a single row")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	model = *mustMonitorModelFromUpdate(t, updated)

	after := model.View()
	if !strings.Contains(after, "model-01") {
		t.Fatalf("expected pgdown to advance logs row in small window")
	}
	if strings.Contains(after, "model-00") {
		t.Fatalf("expected previous logs row to page out in small window")
	}
}

func TestMonitorModel_FooterHelpShowsAccountsOnlyInStats(t *testing.T) {
	collector := observability.NewCollector(10)
	model := NewMonitorModel(&MonitorDeps{Collector: collector}, "127.0.0.1:4000")
	model.width = 200
	model.height = 20

	model.state = tui.ViewStats
	statsView := model.View()
	if !strings.Contains(statsView, "a accounts") {
		t.Fatalf("expected stats footer help to include account shortcut")
	}

	model.state = tui.ViewModels
	modelsView := model.View()
	if strings.Contains(modelsView, "a accounts") {
		t.Fatalf("expected models footer help to hide account shortcut")
	}

	model.state = tui.ViewLogs
	logsView := model.View()
	if strings.Contains(logsView, "a accounts") {
		t.Fatalf("expected logs footer help to hide account shortcut")
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

func TestMonitorModel_LogsViewRendering(t *testing.T) {
	collector := observability.NewCollector(100)
	collector.RecordLocal(&core.RequestRecord{
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
	collector := observability.NewCollector(100)
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
	collector := observability.NewCollector(100)
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

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
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
	if model.serverAddr != "127.0.0.1:4000" {
		t.Fatalf("expected serverAddr to remain runtime listen addr, got %q", model.serverAddr)
	}
	if model.configModal != nil && model.configModal.IsOpen() {
		t.Fatalf("expected modal to close after successful save")
	}
}

func TestMonitorModel_ApplySettingsUsesCallbacks(t *testing.T) {
	collector := observability.NewCollector(100)
	statsSvc := stats.NewService(collector.Observability())
	current := config.DefaultSettings()
	deps := MonitorDeps{
		Collector:    collector,
		StatsService: statsSvc,
		LoadSettings: func() (config.Settings, error) {
			return current, nil
		},
		ApplySettings: func(next config.Settings) (config.Settings, error) {
			current = next
			return current, nil
		},
	}
	model := NewMonitorModel(&deps, "127.0.0.1:4000")
	candidate := current
	candidate.ListenAddr = "127.0.0.1:51234"

	cmd := model.applySettingsCmd(&candidate)
	msg := cmd()
	settingsMsg, ok := msg.(settingsAppliedMsg)
	if !ok {
		t.Fatalf("expected settingsAppliedMsg, got %T", msg)
	}
	if settingsMsg.err != nil {
		t.Fatalf("expected apply to succeed, got %v", settingsMsg.err)
	}
	model.handleSettingsApplied(&settingsMsg)

	if current.ListenAddr != candidate.ListenAddr {
		t.Fatalf("expected callback-backed settings listen addr to update, got %q", current.ListenAddr)
	}
}

func TestMonitorModel_AccountModalOpenOnlyInStats(t *testing.T) {
	collector := observability.NewCollector(100)
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
	collector := observability.NewCollector(100)
	svc := newTestAccountService([]account.AccountDTO{
		{User: "u1", IsDefault: true},
		{User: "u2"},
	}, "u1")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
		UserInfo:       &core.UserInfo{Plan: "business"},
	}
	model := NewMonitorModel(&deps, "")
	model.loadedUserInfo = true
	model.userInfo = &core.UserInfo{Plan: "business"}
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

	if len(svc.switches) != 1 || svc.switches[0] != "u2" {
		t.Fatalf("expected activate callback for u2, got %v", svc.switches)
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

func TestMonitorModel_AccountModalActivateDefersRefreshWhenInFlight(t *testing.T) {
	collector := observability.NewCollector(100)
	svc := newTestAccountService([]account.AccountDTO{
		{User: "u1", HasToken: true, IsDefault: true},
		{User: "u2", HasToken: true},
	}, "u1")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
		UserInfo:       &core.UserInfo{Plan: "business"},
	}
	model := NewMonitorModel(&deps, "")
	model.state = tui.ViewStats

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := model.userInfoQueue.State().Seq
	updated, _ = model.Update(userInfoRefreshDueMsg{seq: seq})
	model = *mustMonitorModelFromUpdate(t, updated)
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected initial refresh to be in-flight")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd != nil {
		t.Fatalf("expected account activation refresh to defer while in-flight")
	}
	if !model.userInfoRefreshAfterInFlight {
		t.Fatalf("expected deferred refresh flag after account activation during in-flight")
	}
	if model.sharedState.UserInfo != nil {
		t.Fatalf("expected shared user info to stay cleared until deferred refresh runs")
	}

	updated, cmd = model.Update(userInfoLoadedMsg{
		info: &core.UserInfo{Plan: "business", Organization: "stale"},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected deferred refresh to start when in-flight request completes")
	}
	if !model.userInfoQueue.State().InFlight {
		t.Fatalf("expected deferred account refresh to become in-flight")
	}
	if model.sharedState.UserInfo != nil {
		t.Fatalf("expected stale in-flight result to be ignored before deferred refresh")
	}
}

func TestMonitorModel_AccountModalActivateFailureKeepsModalOpen(t *testing.T) {
	collector := observability.NewCollector(100)
	svc := newTestAccountService([]account.AccountDTO{
		{User: "u1", IsDefault: true},
		{User: "u2"},
	}, "u1")
	svc.switchErr = errors.New("activate failed")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
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
	collector := observability.NewCollector(100)
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: newTestAccountService(nil, ""),
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
	collector := observability.NewCollector(100)
	svc := newTestAccountService([]account.AccountDTO{
		{User: "u1", IsDefault: true},
	}, "u1")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
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
	seq := int64(1001)

	updated, cmd = model.Update(accountLoginChallengeMsg{
		seq: seq,
		challenge: account.LoginChallenge{
			Seq:             seq,
			VerificationURI: "https://github.com/login/device",
			UserCode:        "ABCD-EFGH",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected poll login command")
	}

	updated, cmd = model.Update(accountLoginResultMsg{
		seq: seq,
		result: account.LoginResult{
			Seq:   seq,
			Token: "ghu-new",
			Login: "u2",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd != nil {
		t.Fatalf("expected no user refresh command when accounts already existed")
	}
	if len(svc.added) != 1 || svc.added[0].User != "u2" || svc.added[0].GhToken != "ghu-new" {
		t.Fatalf("expected add callback with new account, got %+v", svc.added)
	}
	if current, _, err := svc.Current(); err != nil || current.User != "u1" {
		t.Fatalf("expected default account to remain u1, got %v err %v", current, err)
	}
	if got := len(svc.List()); got != 2 {
		t.Fatalf("expected account list to include new account, got %d", got)
	}
	if model.accountModal == nil || !model.accountModal.IsOpen() {
		t.Fatalf("expected account modal to stay open after add success")
	}
	if !strings.Contains(model.accountModal.View(), "u2") {
		t.Fatalf("expected modal to show new account, got:\n%s", model.accountModal.View())
	}
}

func TestMonitorModel_AddAccountFirstAccountTriggersUserRefresh(t *testing.T) {
	collector := observability.NewCollector(100)
	svc := newTestAccountService(nil, "")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Add row is selected by default
	model = *mustMonitorModelFromUpdate(t, updated)
	if cmd == nil {
		t.Fatalf("expected start add-account command")
	}
	seq := int64(1002)

	updated, _ = model.Update(accountLoginChallengeMsg{
		seq: seq,
		challenge: account.LoginChallenge{
			Seq:             seq,
			VerificationURI: "https://github.com/login/device",
			UserCode:        "ABCD-EFGH",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	updated, cmd = model.Update(accountLoginResultMsg{
		seq: seq,
		result: account.LoginResult{
			Seq:   seq,
			Token: "ghu-first",
			Login: "u-first",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if cmd == nil {
		t.Fatalf("expected user-info refresh command when first account is added")
	}
	if current, _, err := svc.Current(); err != nil || current.User != "u-first" {
		t.Fatalf("expected first added account to become default, got %v err %v", current, err)
	}
	if got := len(svc.List()); got != 1 {
		t.Fatalf("expected one account after first add, got %d", got)
	}
}

func TestMonitorModel_AddAccountCancelIgnoresLateMessages(t *testing.T) {
	collector := observability.NewCollector(100)
	svc := newTestAccountService([]account.AccountDTO{{User: "u1", HasToken: true, IsDefault: true}}, "u1")
	deps := MonitorDeps{
		Collector:      collector,
		AccountService: svc,
	}
	model := NewMonitorModel(&deps, "")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = *mustMonitorModelFromUpdate(t, updated)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = *mustMonitorModelFromUpdate(t, updated)
	seq := int64(1003)

	updated, _ = model.Update(accountLoginChallengeMsg{
		seq: seq,
		challenge: account.LoginChallenge{
			Seq:             seq,
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

	updated, _ = model.Update(accountLoginResultMsg{
		seq: seq,
		result: account.LoginResult{
			Seq:   seq,
			Token: "late-token",
			Login: "late-user",
		},
	})
	model = *mustMonitorModelFromUpdate(t, updated)

	if len(svc.added) != 0 {
		t.Fatalf("expected late message to be ignored after cancel")
	}
	if got := len(svc.List()); got != 1 || svc.List()[0].User != "u1" {
		t.Fatalf("expected account list unchanged after cancel, got %+v", svc.List())
	}
}

type testAccountService struct {
	accounts    []account.AccountDTO
	defaultUser string
	added       []config.Account
	switches    []string
	switchErr   error
}

func newTestAccountService(accounts []account.AccountDTO, defaultUser string) *testAccountService {
	tp := &testAccountService{
		accounts:    cloneAccountDTOs(accounts),
		defaultUser: defaultUser,
	}
	return tp
}

func cloneAccountDTOs(input []account.AccountDTO) []account.AccountDTO {
	if len(input) == 0 {
		return nil
	}
	out := make([]account.AccountDTO, len(input))
	copy(out, input)
	return out
}

func (s *testAccountService) List() []account.AccountDTO {
	return cloneAccountDTOs(s.accounts)
}

func (s *testAccountService) Current() (config.Account, bool, error) {
	if s.defaultUser == "" {
		return config.Account{}, false, config.ErrAccountNotFound
	}
	return config.Account{User: s.defaultUser}, true, nil
}

func (s *testAccountService) SwitchDefault(user string) error {
	if s.switchErr != nil {
		return s.switchErr
	}
	for _, acct := range s.accounts {
		if acct.User == user {
			s.defaultUser = user
			s.switches = append(s.switches, user)
			return nil
		}
	}
	return config.ErrAccountNotFound
}

func (s *testAccountService) Add(acct config.Account) error {
	s.added = append(s.added, acct)
	dto := account.AccountDTO{
		User:      acct.User,
		AppID:     acct.AppID,
		HasToken:  acct.GhToken != "",
		IsDefault: acct.User == s.defaultUser || s.defaultUser == "",
	}
	if s.defaultUser == "" {
		s.defaultUser = acct.User
		dto.IsDefault = true
	}
	s.accounts = append(s.accounts, dto)
	return nil
}

func (s *testAccountService) BeginLogin(ctx context.Context) (account.LoginChallenge, error) {
	return account.LoginChallenge{}, nil
}

func (s *testAccountService) PollLogin(ctx context.Context, seq int64) (account.LoginResult, error) {
	return account.LoginResult{}, nil
}

func (s *testAccountService) CancelLogin(seq int64) {}

func (s *testAccountService) PremiumInfo(ctx context.Context, force bool) (core.UserInfo, error) {
	return core.UserInfo{}, nil
}

func (s *testAccountService) InvalidatePremium(user string) {}

func (s *testAccountService) Remove(user string) error {
	for i, acct := range s.accounts {
		if acct.User == user {
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			if s.defaultUser == user {
				s.defaultUser = ""
			}
			return nil
		}
	}
	return config.ErrAccountNotFound
}

package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"agytop/internal/config"
	"agytop/internal/supervisor"
)

// threeStates builds a small, deterministic StateView fixture spanning three
// distinct IDs, display names, scopes, and statuses, so filter tests can
// target each field independently.
func threeStates() []supervisor.StateView {
	return []supervisor.StateView{
		{
			Config: config.SidecarConfig{
				ID:          "alpha",
				DisplayName: "Alpha Daemon",
				Scope:       "workspace",
				Command:     "bash",
			},
			Status: supervisor.StatusRunning,
		},
		{
			Config: config.SidecarConfig{
				ID:          "beta-cron",
				DisplayName: "Beta Cron Job",
				Scope:       "global",
				Builtin:     "schedule",
			},
			Status: supervisor.StatusScheduled,
		},
		{
			Config: config.SidecarConfig{
				ID:          "gamma",
				DisplayName: "Gamma Worker",
				Scope:       "plugin",
				Command:     "bash",
			},
			Status: supervisor.StatusFailed,
		},
	}
}

// newFakeModel wires a Model to the given fake supervisor and delivers the
// WindowSizeMsg that Model.View() requires before it renders anything but
// the loading screen (mirrors newTestModel in stateview_test.go, which this
// file must not redeclare).
func newFakeModel(t *testing.T, fake *fakeSupervisor) Model {
	t.Helper()
	m := NewModel(fake)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	return m
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// runCmd invokes a tea.Cmd and unwraps tea.Batch's single-command envelope
// (tea.BatchMsg) so tests can assert on the underlying message type, e.g.
// stopResultMsg, rather than the batching mechanism around it.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("runCmd: nil tea.Cmd")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		if len(batch) != 1 {
			t.Fatalf("runCmd: BatchMsg has %d cmds, want 1", len(batch))
		}
		return runCmd(t, batch[0])
	}
	return msg
}

// ---------------------------------------------------------------------
// 1. Search / filter
// ---------------------------------------------------------------------

func TestFilterEntersModeAndNarrowsResults(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	if m.filtering {
		t.Fatal("model should not start in filtering mode")
	}

	var model tea.Model = m
	model, _ = model.Update(keyRunes("/"))
	m = model.(Model)
	if !m.filtering {
		t.Fatal("expected '/' to enter filtering mode")
	}

	for _, r := range "alpha" {
		model, _ = model.Update(keyRunes(string(r)))
	}
	m = model.(Model)

	if got := m.filterInput.Value(); got != "alpha" {
		t.Fatalf("filterInput.Value() = %q, want %q", got, "alpha")
	}
	if len(m.filteredStates) != 1 || m.filteredStates[0].Config.ID != "alpha" {
		t.Fatalf("filteredStates = %+v, want only 'alpha'", m.filteredStates)
	}
}

func TestFilterMatchesEachField(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantIDs []string
	}{
		{"matches by ID", "gamma", []string{"gamma"}},
		{"matches by DisplayName", "cron job", []string{"beta-cron"}},
		{"matches by Scope", "plugin", []string{"gamma"}},
		{"matches by Status", "scheduled", []string{"beta-cron"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeSupervisor(threeStates()...)
			m := newFakeModel(t, fake)

			var model tea.Model = m
			model, _ = model.Update(keyRunes("/"))
			for _, r := range tt.filter {
				model, _ = model.Update(keyRunes(string(r)))
			}
			m = model.(Model)

			if len(m.filteredStates) != len(tt.wantIDs) {
				t.Fatalf("filteredStates = %+v, want ids %v", m.filteredStates, tt.wantIDs)
			}
			for i, want := range tt.wantIDs {
				if m.filteredStates[i].Config.ID != want {
					t.Errorf("filteredStates[%d].Config.ID = %q, want %q", i, m.filteredStates[i].Config.ID, want)
				}
			}
		})
	}
}

func TestEscapeClearsFilterAndRestoresFullList(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	var model tea.Model = m
	model, _ = model.Update(keyRunes("/"))
	for _, r := range "gamma" {
		model, _ = model.Update(keyRunes(string(r)))
	}
	m = model.(Model)
	if len(m.filteredStates) != 1 {
		t.Fatalf("expected filter to narrow to 1, got %d", len(m.filteredStates))
	}

	// "enter" exits filtering mode (per the filtering-mode switch in Update);
	// "esc" from normal mode then clears the retained filter text.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(Model)

	if m.filterInput.Value() != "" {
		t.Errorf("filterInput.Value() = %q, want empty after Esc", m.filterInput.Value())
	}
	if len(m.filteredStates) != 3 {
		t.Fatalf("filteredStates len = %d, want 3 after clearing filter", len(m.filteredStates))
	}
}

func TestNoMatchFilterYieldsEmptyListWithoutPanicking(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	var model tea.Model = m
	model, _ = model.Update(keyRunes("/"))
	for _, r := range "no-such-sidecar-xyz" {
		model, _ = model.Update(keyRunes(string(r)))
	}
	m = model.(Model)

	if len(m.filteredStates) != 0 {
		t.Fatalf("filteredStates = %+v, want empty", m.filteredStates)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View()/cursor move panicked on empty filtered list: %v", r)
		}
	}()

	if out := m.View(); out == "" {
		t.Error("View() returned empty output on empty filtered list")
	}

	model, _ = model.Update(keyRunes("j"))
	model, _ = model.Update(keyRunes("k"))
	m = model.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 on empty filtered list", m.cursor)
	}
}

// ---------------------------------------------------------------------
// 2. Navigation bounds
// ---------------------------------------------------------------------

func TestNavigationClampsAtTopAndBottom(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	var model tea.Model = m

	// k at the top must not go negative.
	model, _ = model.Update(keyRunes("k"))
	m = model.(Model)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after 'k' at top, want 0", m.cursor)
	}

	// j to the bottom and one past it must clamp at len-1.
	for i := 0; i < 5; i++ {
		model, _ = model.Update(keyRunes("j"))
	}
	m = model.(Model)
	if want := len(m.filteredStates) - 1; m.cursor != want {
		t.Fatalf("cursor = %d after repeated 'j', want %d (clamped)", m.cursor, want)
	}
}

func TestNavigationOnEmptyListDoesNotPanic(t *testing.T) {
	fake := newFakeSupervisor() // no states at all
	m := newFakeModel(t, fake)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("navigation on empty list panicked: %v", r)
		}
	}()

	var model tea.Model = m
	model, _ = model.Update(keyRunes("j"))
	model, _ = model.Update(keyRunes("k"))
	model, _ = model.Update(keyRunes("G"))
	model, _ = model.Update(keyRunes("g"))
	m = model.(Model)

	if m.cursor != 0 {
		t.Errorf("cursor = %d on empty list, want 0", m.cursor)
	}
	if out := m.View(); out == "" {
		t.Error("View() returned empty output on empty list")
	}
}

func TestNavigationOnFilteredToNothingDoesNotPanic(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	var model tea.Model = m
	model, _ = model.Update(keyRunes("/"))
	for _, r := range "zzz-nomatch" {
		model, _ = model.Update(keyRunes(string(r)))
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("navigation on filtered-to-nothing list panicked: %v", r)
		}
	}()

	model, _ = model.Update(keyRunes("j"))
	model, _ = model.Update(keyRunes("k"))
	m = model.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

// ---------------------------------------------------------------------
// 3. tea.WindowSizeMsg clamping
// ---------------------------------------------------------------------

func TestWindowSizeClampingDoesNotPanic(t *testing.T) {
	sizes := []tea.WindowSizeMsg{
		{Width: 20, Height: 5},
		{Width: 1, Height: 1},
		{Width: 0, Height: 0},
	}

	for _, sz := range sizes {
		name := fmt.Sprintf("%dx%d", sz.Width, sz.Height)
		t.Run(name, func(t *testing.T) {
			fake := newFakeSupervisor(threeStates()...)
			m := NewModel(fake)

			var model tea.Model = m
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Update(%+v) panicked: %v", sz, r)
					}
				}()
				model, _ = model.Update(sz)
			}()
			m = model.(Model)

			// The percentage layout math used to go negative here -- at
			// width 1 the log viewport landed at -4 -- because only height
			// was clamped. Bubbles tolerated it, so nothing panicked and the
			// bug was invisible until asserted directly.
			if w := m.logViewport.Width; w < 1 {
				t.Errorf("logViewport.Width = %d, want >= 1", w)
			}
			if h := m.logViewport.Height; h < 1 {
				t.Errorf("logViewport.Height = %d, want >= 1", h)
			}

			var out string
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("View() after %+v panicked: %v", sz, r)
					}
				}()
				out = m.View()
			}()

			if out == "" {
				t.Errorf("View() returned empty output for size %+v", sz)
			}
		})
	}
}

// ---------------------------------------------------------------------
// 4. Action keys through the fake
// ---------------------------------------------------------------------

func TestActionKeysInvokeSupervisorMethods(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)
	selectedID := m.filteredStates[m.cursor].Config.ID // "alpha"

	var model tea.Model = m

	// s: Start is called synchronously in Update.
	model, _ = model.Update(keyRunes("s"))
	if got := fake.callsTo("Start"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("Start calls = %v, want [%q]", got, selectedID)
	}

	// x: Stop happens inside the returned tea.Cmd, not synchronously. Update
	// wraps every returned cmd in tea.Batch (even a batch of one), so runCmd
	// unwraps the resulting tea.BatchMsg to reach the real message.
	var cmd tea.Cmd
	model, cmd = model.Update(keyRunes("x"))
	if cmd == nil {
		t.Fatal("expected 'x' to return a non-nil tea.Cmd")
	}
	msg := runCmd(t, cmd)
	if _, ok := msg.(stopResultMsg); !ok {
		t.Fatalf("cmd() resolved to %T, want stopResultMsg", msg)
	}
	if got := fake.callsTo("Stop"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("Stop calls = %v, want [%q]", got, selectedID)
	}

	// r: Restart likewise happens inside the returned tea.Cmd.
	model, cmd = model.Update(keyRunes("r"))
	if cmd == nil {
		t.Fatal("expected 'r' to return a non-nil tea.Cmd")
	}
	msg = runCmd(t, cmd)
	if _, ok := msg.(restartResultMsg); !ok {
		t.Fatalf("cmd() resolved to %T, want restartResultMsg", msg)
	}
	if got := fake.callsTo("Restart"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("Restart calls = %v, want [%q]", got, selectedID)
	}

	// d: DryRun is called synchronously.
	model, _ = model.Update(keyRunes("d"))
	if got := fake.callsTo("DryRun"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("DryRun calls = %v, want [%q]", got, selectedID)
	}
	m = model.(Model)
	if !m.dryRunModalOpen {
		t.Error("expected dryRunModalOpen after successful 'd'")
	}
	// Close the modal before the next key so 't' below reaches the main
	// keybinding switch instead of the modal's own guard block.
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// t: TriggerScheduled is called synchronously.
	model, _ = model.Update(keyRunes("t"))
	if got := fake.callsTo("TriggerScheduled"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("TriggerScheduled calls = %v, want [%q]", got, selectedID)
	}

	// c: ClearLogs is called synchronously.
	model, _ = model.Update(keyRunes("c"))
	if got := fake.callsTo("ClearLogs"); len(got) != 1 || got[0] != selectedID {
		t.Errorf("ClearLogs calls = %v, want [%q]", got, selectedID)
	}
}

// ---------------------------------------------------------------------
// 5. Async result messages
// ---------------------------------------------------------------------

func TestStopResultMsgSetsNotification(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	updated, _ := m.Update(stopResultMsg{displayName: "Alpha Daemon", err: nil})
	m = updated.(Model)
	if !strings.Contains(m.notification, "Stopped") || !strings.Contains(m.notification, "Alpha Daemon") {
		t.Errorf("notification = %q, want it to mention Stopped + Alpha Daemon", m.notification)
	}

	updated, _ = m.Update(stopResultMsg{displayName: "Alpha Daemon", err: errors.New("boom")})
	m = updated.(Model)
	if !strings.Contains(m.notification, "Error stopping") || !strings.Contains(m.notification, "boom") {
		t.Errorf("notification = %q, want it to mention the stop error", m.notification)
	}
}

func TestRestartResultMsgSetsNotification(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	m := newFakeModel(t, fake)

	updated, _ := m.Update(restartResultMsg{displayName: "Gamma Worker", err: nil})
	m = updated.(Model)
	if !strings.Contains(m.notification, "Restarted") || !strings.Contains(m.notification, "Gamma Worker") {
		t.Errorf("notification = %q, want it to mention Restarted + Gamma Worker", m.notification)
	}

	updated, _ = m.Update(restartResultMsg{displayName: "Gamma Worker", err: errors.New("kaboom")})
	m = updated.(Model)
	if !strings.Contains(m.notification, "Error restarting") || !strings.Contains(m.notification, "kaboom") {
		t.Errorf("notification = %q, want it to mention the restart error", m.notification)
	}
}

func TestStopSidecarCmdReturnsExpectedMsg(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	cmd := stopSidecarCmd(fake, "alpha", "Alpha Daemon")
	msg := cmd()

	got, ok := msg.(stopResultMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want stopResultMsg", msg)
	}
	if got.displayName != "Alpha Daemon" || got.err != nil {
		t.Errorf("got %+v, want displayName=Alpha Daemon, err=nil", got)
	}
	if ids := fake.callsTo("Stop"); len(ids) != 1 || ids[0] != "alpha" {
		t.Errorf("Stop calls = %v, want [alpha]", ids)
	}
}

func TestStopSidecarCmdPropagatesError(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	fake.setErr("Stop", errors.New("stop failed"))
	cmd := stopSidecarCmd(fake, "alpha", "Alpha Daemon")
	msg := cmd()

	got, ok := msg.(stopResultMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want stopResultMsg", msg)
	}
	if got.err == nil || got.err.Error() != "stop failed" {
		t.Errorf("got err = %v, want \"stop failed\"", got.err)
	}
}

func TestRestartSidecarCmdReturnsExpectedMsg(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	cmd := restartSidecarCmd(fake, "beta-cron", "Beta Cron Job")
	msg := cmd()

	got, ok := msg.(restartResultMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want restartResultMsg", msg)
	}
	if got.displayName != "Beta Cron Job" || got.err != nil {
		t.Errorf("got %+v, want displayName=Beta Cron Job, err=nil", got)
	}
	if ids := fake.callsTo("Restart"); len(ids) != 1 || ids[0] != "beta-cron" {
		t.Errorf("Restart calls = %v, want [beta-cron]", ids)
	}
}

func TestRestartSidecarCmdPropagatesError(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	fake.setErr("Restart", errors.New("restart failed"))
	cmd := restartSidecarCmd(fake, "beta-cron", "Beta Cron Job")
	msg := cmd()

	got, ok := msg.(restartResultMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want restartResultMsg", msg)
	}
	if got.err == nil || got.err.Error() != "restart failed" {
		t.Errorf("got err = %v, want \"restart failed\"", got.err)
	}
}

// ---------------------------------------------------------------------
// Action keys surface injected errors as notifications too.
// ---------------------------------------------------------------------

func TestActionKeyErrorsSurfaceAsNotifications(t *testing.T) {
	fake := newFakeSupervisor(threeStates()...)
	fake.setErr("Start", errors.New("start failed"))
	fake.setErr("TriggerScheduled", errors.New("trigger failed"))
	fake.setErr("ClearLogs", errors.New("clear failed"))
	fake.setErr("DryRun", errors.New("dryrun failed"))
	m := newFakeModel(t, fake)

	var model tea.Model = m

	model, _ = model.Update(keyRunes("s"))
	m = model.(Model)
	if !strings.Contains(m.notification, "start failed") {
		t.Errorf("notification = %q, want it to mention start failed", m.notification)
	}

	model, _ = model.Update(keyRunes("t"))
	m = model.(Model)
	if !strings.Contains(m.notification, "trigger failed") {
		t.Errorf("notification = %q, want it to mention trigger failed", m.notification)
	}

	model, _ = model.Update(keyRunes("c"))
	m = model.(Model)
	if !strings.Contains(m.notification, "clear failed") {
		t.Errorf("notification = %q, want it to mention clear failed", m.notification)
	}

	model, _ = model.Update(keyRunes("d"))
	m = model.(Model)
	if !strings.Contains(m.notification, "dryrun failed") {
		t.Errorf("notification = %q, want it to mention dryrun failed", m.notification)
	}
	if m.dryRunModalOpen {
		t.Error("dryRunModalOpen should stay false when DryRun errors")
	}
}

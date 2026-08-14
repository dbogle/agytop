package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"agytop/internal/config"
	"agytop/internal/supervisor"
)

// These tests guard the supervisor.SidecarState -> supervisor.StateView
// refactor that removed the copied sync.RWMutex from the UI boundary. They
// exercise the render and keybinding paths that were retyped, which
// compilation alone does not validate.

func newTestModel(t *testing.T) Model {
	t.Helper()

	cfgs := []config.SidecarConfig{
		{
			ID:            "alpha",
			DisplayName:   "Alpha Daemon",
			Command:       "bash",
			Args:          []string{"-c", "true"},
			Scope:         "custom",
			RestartPolicy: config.RestartAlways,
		},
		{
			ID:            "beta",
			DisplayName:   "Beta Cron",
			Builtin:       "schedule",
			Schedule:      "0 0 * * *",
			Scope:         "custom",
			RestartPolicy: config.RestartNever,
		},
	}

	// NewRegistryAt keeps the test off the real ~/.agytop.
	sup := supervisor.NewSupervisorWithRegistry(cfgs, supervisor.NewRegistryAt(t.TempDir()))

	m := NewModel(sup)
	// View() short-circuits to a loading screen until a WindowSizeMsg arrives,
	// so dimensions must be delivered as a message rather than set directly.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	m.refreshStates()
	return m
}

func TestViewRendersStateViews(t *testing.T) {
	m := newTestModel(t)

	out := m.View()
	if out == "" {
		t.Fatal("View() returned empty output")
	}
	for _, want := range []string{"Alpha Daemon", "Beta Cron"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in rendered view", want)
		}
	}
}

func TestModalsRenderStateViewByValue(t *testing.T) {
	m := newTestModel(t)

	cur := m.selectedState()
	if cur == nil {
		t.Fatal("selectedState() returned nil")
	}

	cur.RunHistory = []supervisor.RunRecord{
		{Timestamp: time.Now().Add(-time.Minute), ExitCode: 0, Duration: 120 * time.Millisecond},
		{Timestamp: time.Now(), ExitCode: 1, Duration: 80 * time.Millisecond, Error: "boom"},
	}

	total, rate, successes, failures := cur.GetRunStats()
	if total != 2 || successes != 1 || failures != 1 || rate != 50.0 {
		t.Errorf("GetRunStats() = (%d, %v, %d, %d), want (2, 50, 1, 1)", total, rate, successes, failures)
	}

	// Both modals take a StateView by value -- the signatures that previously
	// tripped `go vet`'s copylocks check.
	if RenderConfigModal(*cur, 120, 40) == "" {
		t.Error("RenderConfigModal returned empty output")
	}
	if RenderRunHistoryModal(*cur, 120, 40) == "" {
		t.Error("RenderRunHistoryModal returned empty output")
	}
}

// The zero-run branch returns a 100.0 success rate rather than dividing by
// zero; it is the case most easily dropped when GetRunStats moved off
// *SidecarState onto StateView.
func TestGetRunStatsEmptyViewKeepsDefault(t *testing.T) {
	var v supervisor.StateView

	total, rate, successes, failures := v.GetRunStats()
	if total != 0 || rate != 100.0 || successes != 0 || failures != 0 {
		t.Errorf("GetRunStats() = (%d, %v, %d, %d), want (0, 100, 0, 0)", total, rate, successes, failures)
	}
}

// Drives the keys that read or mutate the retyped state, including "c" (clear
// logs) and the modal toggles that pass a StateView by value.
func TestKeyRoutingOverStateView(t *testing.T) {
	m := newTestModel(t)

	keys := []string{"j", "k", "tab", "h", "h", "v", "v", "c", "?", "?"}

	var model tea.Model = m
	for _, k := range keys {
		msg := tea.Msg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if k == "tab" {
			msg = tea.KeyMsg{Type: tea.KeyTab}
		}

		model, _ = model.Update(msg)
		if model.(Model).View() == "" {
			t.Fatalf("View() returned empty output after key %q", k)
		}
	}
}

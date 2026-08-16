package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"agytop/internal/config"
	"agytop/internal/supervisor"
)

func TestErrorFilterKeyTogglesFilter(t *testing.T) {
	cfgs := []config.SidecarConfig{
		{
			ID:          "srv",
			DisplayName: "Service",
			Command:     "echo",
			Args:        []string{"hello"},
		},
	}
	sup := supervisor.NewSupervisorWithRegistry(cfgs, supervisor.NewRegistryAt(t.TempDir()))
	m := NewModel(sup)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	cur := m.selectedState()
	if cur == nil {
		t.Fatal("expected selected state")
	}

	// Add normal and error log entries
	cur.Logs = []supervisor.LogEntry{
		{Timestamp: time.Now(), Source: supervisor.SourceStdout, Text: "Normal info line"},
		{Timestamp: time.Now(), Source: supervisor.SourceStderr, Text: "Error executing agentapi: failed"},
	}

	// Initially show all
	m.updateLogContent()
	if !strings.Contains(m.logViewport.View(), "Normal info line") {
		t.Errorf("expected normal log in initial view")
	}

	// Press 'e' to toggle errors-only filter
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	if !m.logErrorsOnly {
		t.Errorf("expected logErrorsOnly to be true after pressing 'e'")
	}
	m.updateLogContent()
	view := m.logViewport.View()
	if strings.Contains(view, "Normal info line") {
		t.Errorf("expected normal log to be filtered out in errors-only view")
	}
	if !strings.Contains(view, "Error executing agentapi: failed") {
		t.Errorf("expected error log to be preserved in errors-only view")
	}

	// Press 'e' again to toggle back
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(Model)
	if m.logErrorsOnly {
		t.Errorf("expected logErrorsOnly to be false after pressing 'e' again")
	}
}

func TestInspectorRendersScheduleDomainAndAgent(t *testing.T) {
	cfgs := []config.SidecarConfig{
		{
			ID:          "sentinel",
			DisplayName: "E2E Sentinel",
			Command:     "python3",
			Args:        []string{"scanner.py", "--daemon", "--hour", "4", "--minute", "0"},
		},
	}
	sup := supervisor.NewSupervisorWithRegistry(cfgs, supervisor.NewRegistryAt(t.TempDir()))
	m := NewModel(sup)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	cur := m.selectedState()
	cur.DomainState = &supervisor.DomainState{
		LastStatus:       "passing",
		LastRunTimestamp: "2026-08-16T02:22:19Z",
	}
	cur.ScheduleText = "Daily @ 04:00"
	cur.NextScheduleRun = time.Now().Add(4 * time.Hour)
	cur.HasSchedule = true
	cur.AgentConversationID = "dcd004c9-a6ed-450f-891b-a487b67dc655"
	cur.AgentConversationTitle = "E2E Smoke Sentinel (2026-08-16)"

	out := m.renderInspectorPane(80, 20)
	if !strings.Contains(out, "OUTCOME:") || !strings.Contains(out, "PASSING") {
		t.Errorf("expected OUTCOME and PASSING in inspector output:\n%s", out)
	}
	if !strings.Contains(out, "SCHEDULE:") || !strings.Contains(out, "Daily @ 04:00") {
		t.Errorf("expected SCHEDULE: Daily @ 04:00 in inspector output:\n%s", out)
	}
	if !strings.Contains(out, "AGENT") || !strings.Contains(out, "dcd004c9-a6ed-450f-891b-a487b67dc655") {
		t.Errorf("expected AGENT and conversation ID in inspector output:\n%s", out)
	}
}

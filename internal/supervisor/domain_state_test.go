package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"agytop/internal/config"
)

func TestDomainStateAndAgentConversationExtraction(t *testing.T) {
	tmpDir := t.TempDir()

	// Write sidecar's state.json
	stateJSON := `{
  "last_run_timestamp": "2026-08-16T02:22:19.549159+00:00",
  "last_status": "passing"
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatalf("failed to write state.json: %v", err)
	}

	cfg := config.SidecarConfig{
		ID:        "e2e-smoke-sentinel",
		Directory: tmpDir,
		Command:   "python3",
		Args:      []string{"scanner.py", "--daemon", "--hour", "4", "--minute", "0"},
	}

	state := NewSidecarState(cfg)
	if state.DomainState == nil {
		t.Fatalf("expected DomainState to be loaded from state.json")
	}
	if state.DomainState.LastStatus != "passing" {
		t.Errorf("expected LastStatus 'passing', got %q", state.DomainState.LastStatus)
	}

	// Add log entry containing conversationId and title
	logLine := `[e2e-smoke-sentinel] Dispatched conversation: {"response": {"newConversation": {"conversationId": "dcd004c9-a6ed-450f-891b-a487b67dc655"}}}`
	state.AddLog(SourceStdout, logLine)

	view := state.Snapshot()
	if view.DomainState == nil || view.DomainState.LastStatus != "passing" {
		t.Errorf("expected snapshot to carry domain state passing")
	}
	if view.AgentConversationID != "dcd004c9-a6ed-450f-891b-a487b67dc655" {
		t.Errorf("expected AgentConversationID 'dcd004c9-a6ed-450f-891b-a487b67dc655', got %q", view.AgentConversationID)
	}
	if view.ScheduleText != "Daily @ 04:00" {
		t.Errorf("expected ScheduleText 'Daily @ 04:00', got %q", view.ScheduleText)
	}
}

func TestTriggerDaemonOneOffExecution(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "scanner.sh")
	scriptContent := `#!/bin/sh
if [ "$1" = "--run-now" ]; then
    echo "One-off run completed"
    exit 0
fi
echo "Unexpected arg: $1"
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	cfg := config.SidecarConfig{
		ID:            "test-daemon",
		DisplayName:   "Test Daemon",
		Command:       scriptPath,
		Args:          []string{"--daemon", "--weekday", "2", "--hour", "2", "--minute", "0"},
		WorkingDir:    tmpDir,
		RestartPolicy: config.RestartNever,
	}

	sup := NewSupervisorWithRegistry([]config.SidecarConfig{cfg}, NewRegistryAt(t.TempDir()))
	defer sup.Shutdown()

	if err := sup.TriggerScheduled("test-daemon"); err != nil {
		t.Fatalf("TriggerScheduled failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		st, ok := sup.GetState("test-daemon")
		return ok && len(st.RunHistory) >= 1 && st.RunHistory[0].ExitCode == 0
	}, "daemon one-off execution to record run with exit code 0")

	st, _ := sup.GetState("test-daemon")
	if len(st.RunHistory) == 0 || st.RunHistory[0].Snippet != "One-off run completed" {
		t.Errorf("expected snippet 'One-off run completed', got %+v", st.RunHistory)
	}
}

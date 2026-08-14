package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"antigravity-sidecars/internal/config"
)

func TestSupervisorLifecycleAndDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "worker.sh")
	scriptContent := `#!/bin/sh
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "Dry run mode active. Validation OK."
    exit 0
fi
while true; do
    echo "Worker tick"
    sleep 0.1
done
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	cfg := config.SidecarConfig{
		ID:            "test-worker",
		DisplayName:   "Test Worker Daemon",
		Command:       scriptPath,
		RestartPolicy: config.RestartNever,
		WorkingDir:    tmpDir,
		Env: map[string]string{
			"WORKER_MODE": "test",
		},
	}

	sup := NewSupervisor([]config.SidecarConfig{cfg})
	defer sup.Shutdown()

	// Test 1: Dry-Run execution
	dryRun, err := sup.DryRun("test-worker")
	if err != nil {
		t.Fatalf("DryRun returned error: %v", err)
	}
	if !dryRun.Success {
		t.Errorf("expected dry-run to succeed, got messages: %v", dryRun.ValidationMsgs)
	}
	if dryRun.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", dryRun.ExitCode)
	}

	foundDryRunLog := false
	for _, l := range dryRun.Logs {
		if l.Text == "Dry run mode active. Validation OK." {
			foundDryRunLog = true
			break
		}
	}
	if !foundDryRunLog {
		t.Errorf("dry run did not capture expected log output: %v", dryRun.Logs)
	}

	// Test 2: Start process
	if err := sup.Start("test-worker"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(250 * time.Millisecond)

	state, ok := sup.GetState("test-worker")
	if !ok {
		t.Fatalf("could not retrieve state")
	}
	if state.Status != StatusRunning {
		t.Errorf("expected state RUNNING, got %s", state.Status)
	}
	if state.PID <= 0 {
		t.Errorf("expected positive PID, got %d", state.PID)
	}

	// Test 3: Stop process
	if err := sup.Stop("test-worker"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	state, _ = sup.GetState("test-worker")
	if state.Status != StatusStopped {
		t.Errorf("expected state STOPPED, got %s", state.Status)
	}
}

func TestSupervisorScheduledLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "cron_job.sh")
	scriptContent := `#!/bin/sh
echo "Cron tick at $(date)"
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	cfg := config.SidecarConfig{
		ID:          "test-cron",
		DisplayName: "Test Cron Job",
		Builtin:     "schedule",
		Command:     scriptPath,
		Schedule:    "0 0 * * *",
		WorkingDir:  tmpDir,
	}

	sup := NewSupervisor([]config.SidecarConfig{cfg})
	defer sup.Shutdown()

	state, _ := sup.GetState("test-cron")
	if state.Status != StatusStopped {
		t.Errorf("expected initial status STOPPED, got %s", state.Status)
	}

	// Arm scheduler
	if err := sup.Start("test-cron"); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	state, _ = sup.GetState("test-cron")
	if state.Status != StatusScheduled {
		t.Errorf("expected armed status SCHEDULED, got %s", state.Status)
	}

	// Trigger execution
	if err := sup.TriggerScheduled("test-cron"); err != nil {
		t.Fatalf("TriggerScheduled failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	state, _ = sup.GetState("test-cron")
	if state.Status != StatusScheduled {
		t.Errorf("expected status to return to SCHEDULED after run, got %s", state.Status)
	}

	// Stop scheduler
	if err := sup.Stop("test-cron"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	state, _ = sup.GetState("test-cron")
	if state.Status != StatusStopped {
		t.Errorf("expected status STOPPED when paused, got %s", state.Status)
	}
}

package supervisor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"agytop/internal/config"
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

func TestSupervisorRunHistoryAndStats(t *testing.T) {
	tmpDir := t.TempDir()
	successScript := filepath.Join(tmpDir, "success.sh")
	_ = os.WriteFile(successScript, []byte("#!/bin/sh\necho 'Report complete'\nexit 0\n"), 0755)

	failScript := filepath.Join(tmpDir, "fail.sh")
	_ = os.WriteFile(failScript, []byte("#!/bin/sh\necho 'Database error'\nexit 2\n"), 0755)

	cfgSuccess := config.SidecarConfig{
		ID:          "cron-success",
		DisplayName: "Cron Success",
		Builtin:     "schedule",
		Command:     successScript,
		Schedule:    "0 0 * * *",
		WorkingDir:  tmpDir,
	}

	cfgFail := config.SidecarConfig{
		ID:          "cron-fail",
		DisplayName: "Cron Fail",
		Builtin:     "schedule",
		Command:     failScript,
		Schedule:    "0 0 * * *",
		WorkingDir:  tmpDir,
	}

	sup := NewSupervisor([]config.SidecarConfig{cfgSuccess, cfgFail})
	defer sup.Shutdown()

	// Trigger success 2 times
	_ = sup.TriggerScheduledWithSource("cron-success", TriggerManual)
	time.Sleep(150 * time.Millisecond)
	_ = sup.TriggerScheduledWithSource("cron-success", TriggerCron)
	time.Sleep(150 * time.Millisecond)

	state, _ := sup.GetState("cron-success")
	history := state.GetRunHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 run history records, got %d", len(history))
	}
	if history[0].Trigger != TriggerManual || history[1].Trigger != TriggerCron {
		t.Errorf("expected triggers MANUAL and CRON, got %v and %v", history[0].Trigger, history[1].Trigger)
	}
	if history[0].ExitCode != 0 || history[1].ExitCode != 0 {
		t.Errorf("expected exit codes 0, got %d and %d", history[0].ExitCode, history[1].ExitCode)
	}
	if history[0].Snippet != "Report complete" {
		t.Errorf("expected snippet 'Report complete', got '%s'", history[0].Snippet)
	}

	total, rate, succ, fail := state.GetRunStats()
	if total != 2 || rate != 100.0 || succ != 2 || fail != 0 {
		t.Errorf("expected (2, 100%%, 2, 0), got (%d, %.1f%%, %d, %d)", total, rate, succ, fail)
	}

	// Trigger failure
	_ = sup.TriggerScheduledWithSource("cron-fail", TriggerCron)
	time.Sleep(150 * time.Millisecond)

	failState, _ := sup.GetState("cron-fail")
	failHistory := failState.GetRunHistory()
	if len(failHistory) != 1 {
		t.Fatalf("expected 1 run history record, got %d", len(failHistory))
	}
	if failHistory[0].ExitCode != 2 {
		t.Errorf("expected exit code 2, got %d", failHistory[0].ExitCode)
	}
	totalF, rateF, succF, failF := failState.GetRunStats()
	if totalF != 1 || rateF != 0.0 || succF != 0 || failF != 1 {
		t.Errorf("expected (1, 0%%, 0, 1), got (%d, %.1f%%, %d, %d)", totalF, rateF, succF, failF)
	}
}

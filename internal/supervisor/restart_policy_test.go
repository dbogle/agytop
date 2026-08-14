package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"agytop/internal/config"
)

// writeExitScript writes a tiny shell script under dir that always exits
// with the given code, for use as a fast, deterministic sidecar command in
// restart-policy tests.
func writeExitScript(t *testing.T, dir string, name string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write script %s: %v", path, err)
	}
	return path
}

// newFastSupervisor builds a hermetic supervisor (temp-dir registry) with
// baseBackoff/maxBackoff shrunk to single-digit milliseconds so
// restart-policy and backoff-growth tests don't burn wall-clock time.
func newFastSupervisor(t *testing.T, cfgs []config.SidecarConfig) *Supervisor {
	t.Helper()
	sup := NewSupervisorWithRegistry(cfgs, NewRegistryAt(t.TempDir()))
	sup.baseBackoff = 5 * time.Millisecond
	sup.maxBackoff = 20 * time.Millisecond
	t.Cleanup(sup.ShutdownAndStopAll)
	return sup
}

// TestRestartAlwaysRestartsRegardlessOfExitCode verifies that RestartAlways
// keeps restarting the process after both a zero exit and a non-zero exit,
// incrementing Restarts each time.
func TestRestartAlwaysRestartsRegardlessOfExitCode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		exitCode int
	}{
		{"zero exit", 0},
		{"non-zero exit", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			script := writeExitScript(t, tmpDir, "worker.sh", tc.exitCode)

			cfg := config.SidecarConfig{
				ID:            "restart-always",
				DisplayName:   "Restart Always",
				Command:       script,
				WorkingDir:    tmpDir,
				RestartPolicy: config.RestartAlways,
			}

			sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
			if err := sup.Start("restart-always"); err != nil {
				t.Fatalf("Start failed: %v", err)
			}

			waitFor(t, 5*time.Second, func() bool {
				st, ok := sup.GetState("restart-always")
				return ok && st.Restarts >= 2
			}, "at least 2 restarts under RestartAlways")

			st, _ := sup.GetState("restart-always")
			if st.Restarts < 2 {
				t.Errorf("expected Restarts >= 2, got %d", st.Restarts)
			}
		})
	}
}

// TestRestartOnFailurePolicy verifies RestartOnFailure restarts after a
// non-zero exit but not after a zero exit.
func TestRestartOnFailurePolicy(t *testing.T) {
	t.Run("exit code 0 does not restart", func(t *testing.T) {
		tmpDir := t.TempDir()
		script := writeExitScript(t, tmpDir, "worker.sh", 0)

		cfg := config.SidecarConfig{
			ID:            "on-failure-clean",
			DisplayName:   "On Failure Clean Exit",
			Command:       script,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartOnFailure,
		}

		sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
		if err := sup.Start("on-failure-clean"); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		// Wait for the terminal state directly (no restart, so runProcessLoop
		// sets StatusStopped once and returns).
		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("on-failure-clean")
			return ok && st.Status == StatusStopped
		}, "process to reach terminal STOPPED state on clean exit")

		// Give any (incorrect) restart a moment it would need to happen, by
		// polling that Restarts stays at 0 across a short observation window
		// rather than sleeping blindly -- if a restart erroneously occurs,
		// Restarts flips to a nonzero value almost immediately since backoff
		// is single-digit ms.
		deadline := time.Now().Add(100 * time.Millisecond)
		for time.Now().Before(deadline) {
			st, _ := sup.GetState("on-failure-clean")
			if st.Restarts != 0 {
				t.Fatalf("expected no restarts after clean exit under RestartOnFailure, got %d", st.Restarts)
			}
			time.Sleep(5 * time.Millisecond)
		}

		st, _ := sup.GetState("on-failure-clean")
		if st.Status != StatusStopped {
			t.Errorf("expected StatusStopped after clean exit, got %s", st.Status)
		}
	})

	t.Run("exit code 1 restarts", func(t *testing.T) {
		tmpDir := t.TempDir()
		script := writeExitScript(t, tmpDir, "worker.sh", 1)

		cfg := config.SidecarConfig{
			ID:            "on-failure-crash",
			DisplayName:   "On Failure Crash Exit",
			Command:       script,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartOnFailure,
		}

		sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
		if err := sup.Start("on-failure-crash"); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("on-failure-crash")
			return ok && st.Restarts >= 2
		}, "at least 2 restarts under RestartOnFailure with non-zero exit")

		st, _ := sup.GetState("on-failure-crash")
		if st.Restarts < 2 {
			t.Errorf("expected Restarts >= 2, got %d", st.Restarts)
		}
	})
}

// TestRestartNeverPolicy verifies RestartNever never restarts, and that the
// terminal status reflects the exit code: StatusFailed on non-zero exit,
// StatusStopped on zero exit (per supervisor.go's !shouldRestart branch).
func TestRestartNeverPolicy(t *testing.T) {
	t.Run("zero exit ends STOPPED", func(t *testing.T) {
		tmpDir := t.TempDir()
		script := writeExitScript(t, tmpDir, "worker.sh", 0)

		cfg := config.SidecarConfig{
			ID:            "never-clean",
			DisplayName:   "Never Clean Exit",
			Command:       script,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartNever,
		}

		sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
		if err := sup.Start("never-clean"); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("never-clean")
			return ok && st.Status == StatusStopped
		}, "process to reach STOPPED on clean exit under RestartNever")

		st, _ := sup.GetState("never-clean")
		if st.Status != StatusStopped {
			t.Errorf("expected StatusStopped, got %s", st.Status)
		}
		if st.Restarts != 0 {
			t.Errorf("expected 0 restarts under RestartNever, got %d", st.Restarts)
		}
	})

	t.Run("non-zero exit ends FAILED", func(t *testing.T) {
		tmpDir := t.TempDir()
		script := writeExitScript(t, tmpDir, "worker.sh", 1)

		cfg := config.SidecarConfig{
			ID:            "never-crash",
			DisplayName:   "Never Crash Exit",
			Command:       script,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartNever,
		}

		sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
		if err := sup.Start("never-crash"); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("never-crash")
			return ok && st.Status == StatusFailed
		}, "process to reach FAILED on non-zero exit under RestartNever")

		st, _ := sup.GetState("never-crash")
		if st.Status != StatusFailed {
			t.Errorf("expected StatusFailed, got %s", st.Status)
		}
		if st.Restarts != 0 {
			t.Errorf("expected 0 restarts under RestartNever, got %d", st.Restarts)
		}
	})
}

// backoffLogRe matches the supervisor log line emitted right before each
// restart sleep: "Process exited with code 1. Restarting in 10ms (Restart #2)..."
// Parsing the logged duration lets the test observe the backoff value the
// code actually used, rather than measuring wall-clock gaps between restarts
// -- so the test is immune to scheduler jitter/load.
var backoffLogRe = regexp.MustCompile(`Restarting in (\S+) \(Restart #(\d+)\)`)

// TestBackoffGrowsAndCaps verifies that with baseBackoff/maxBackoff shrunk
// via the Supervisor fields, the backoff used between restarts doubles on
// each successive restart and is capped at maxBackoff, by parsing the
// backoff duration the supervisor itself logged before each sleep.
func TestBackoffGrowsAndCaps(t *testing.T) {
	tmpDir := t.TempDir()
	script := writeExitScript(t, tmpDir, "worker.sh", 1)

	cfg := config.SidecarConfig{
		ID:            "backoff-growth",
		DisplayName:   "Backoff Growth",
		Command:       script,
		WorkingDir:    tmpDir,
		RestartPolicy: config.RestartAlways,
	}

	sup := newFastSupervisor(t, []config.SidecarConfig{cfg})
	// baseBackoff=5ms, maxBackoff=20ms => logged sequence should be
	// 5ms, 10ms, 20ms, 20ms, ... (doubling then capped at 20ms).
	if err := sup.Start("backoff-growth"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		st, ok := sup.GetState("backoff-growth")
		return ok && st.Restarts >= 4
	}, "at least 4 restarts to observe backoff growth and cap")

	st, _ := sup.GetState("backoff-growth")

	var backoffs []time.Duration
	for _, entry := range st.Logs {
		m := backoffLogRe.FindStringSubmatch(entry.Text)
		if m == nil {
			continue
		}
		d, err := time.ParseDuration(m[1])
		if err != nil {
			t.Fatalf("could not parse logged backoff %q: %v", m[1], err)
		}
		backoffs = append(backoffs, d)
	}

	if len(backoffs) < 4 {
		t.Fatalf("expected at least 4 logged backoff values, got %d: %v", len(backoffs), backoffs)
	}

	// Growth: each value must be >= the previous (non-decreasing), and it
	// must actually grow past the base at least once before capping.
	grew := false
	for i := 1; i < len(backoffs); i++ {
		if backoffs[i] < backoffs[i-1] {
			t.Fatalf("backoff decreased at restart %d: %v -> %v (full sequence: %v)", i+1, backoffs[i-1], backoffs[i], backoffs)
		}
		if backoffs[i] > backoffs[i-1] {
			grew = true
		}
	}
	if !grew {
		t.Errorf("expected backoff to grow across restarts, got constant sequence: %v", backoffs)
	}

	// Cap: no logged backoff may exceed maxBackoff, and the tail of the
	// sequence (once growth would have exceeded the cap) must sit exactly
	// at maxBackoff.
	for i, d := range backoffs {
		if d > sup.maxBackoff {
			t.Errorf("backoff at restart %d (%v) exceeds maxBackoff (%v)", i+1, d, sup.maxBackoff)
		}
	}
	last := backoffs[len(backoffs)-1]
	if last != sup.maxBackoff {
		t.Errorf("expected backoff to have capped at maxBackoff (%v) by the last observed restart, got %v (full sequence: %v)", sup.maxBackoff, last, backoffs)
	}
}

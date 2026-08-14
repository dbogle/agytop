package supervisor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agytop/internal/config"
)

// TestGetAllStatesConcurrentWithLiveGoroutines exercises GetAllStates()
// under -race while the supervisor's own background goroutines
// (runProcessLoop, metricsLoop, TailFile) are actively mutating the live
// SidecarState concurrently. Before this test, internal/ui (which was the
// only consumer polling GetAllStates()) had 0% coverage, so nothing had
// ever hammered the supervisor -> StateView snapshot boundary from multiple
// reader goroutines at once. It must be meaningful under -race (i.e.
// actually read fields that mutate concurrently) and finish quickly.
func TestGetAllStatesConcurrentWithLiveGoroutines(t *testing.T) {
	tmpDir := t.TempDir()

	// A chatty, fast-looping worker so stdout logging (-> TailFile) and
	// metricsLoop's /proc reads both stay busy for the duration of the test.
	chattyScript := filepath.Join(tmpDir, "chatty.sh")
	if err := os.WriteFile(chattyScript, []byte(
		"#!/bin/sh\nwhile true; do echo tick; sleep 0.01; done\n",
	), 0755); err != nil {
		t.Fatalf("failed to write chatty script: %v", err)
	}

	// A second sidecar that crash-loops quickly under RestartAlways, so
	// Status/PID/Restarts churn heavily on top of the log/metric churn from
	// the chatty one.
	crashyScript := filepath.Join(tmpDir, "crashy.sh")
	if err := os.WriteFile(crashyScript, []byte(
		"#!/bin/sh\nexit 1\n",
	), 0755); err != nil {
		t.Fatalf("failed to write crashy script: %v", err)
	}

	cfgs := []config.SidecarConfig{
		{
			ID:            "chatty",
			DisplayName:   "Chatty Worker",
			Command:       chattyScript,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartNever,
		},
		{
			ID:            "crashy",
			DisplayName:   "Crashy Worker",
			Command:       crashyScript,
			WorkingDir:    tmpDir,
			RestartPolicy: config.RestartAlways,
		},
	}

	sup := NewSupervisorWithRegistry(cfgs, NewRegistryAt(t.TempDir()))
	sup.baseBackoff = 5 * time.Millisecond
	sup.maxBackoff = 15 * time.Millisecond
	// Plain Shutdown() only stops background tickers -- detached (Setsid)
	// child processes are deliberately left running so they survive the TUI
	// exiting (see Supervisor.Shutdown's doc comment). The "chatty" worker
	// here is a genuine long-running daemon, so it must be explicitly
	// stopped via ShutdownAndStopAll or it leaks as an orphaned background
	// process after the test -- which also raced with this test's own
	// t.TempDir() cleanup (its log file lived under the registry dir) and
	// caused an intermittent "directory not empty" failure under -count>1.
	t.Cleanup(func() { stopAndWait(t, sup, "chatty", "crashy") })

	if err := sup.Start("chatty"); err != nil {
		t.Fatalf("Start(chatty) failed: %v", err)
	}
	if err := sup.Start("crashy"); err != nil {
		t.Fatalf("Start(crashy) failed: %v", err)
	}

	// Make sure both runProcessLoop goroutines (and, for chatty, TailFile
	// and metricsLoop's /proc sampling) are actually live before hammering
	// GetAllStates, rather than racing the readers against sidecar startup.
	waitFor(t, 5*time.Second, func() bool {
		st, ok := sup.GetState("chatty")
		return ok && st.Status == StatusRunning && st.PID > 0
	}, "chatty worker to be RUNNING before hammering GetAllStates")
	waitFor(t, 5*time.Second, func() bool {
		st, ok := sup.GetState("crashy")
		return ok && st.Restarts >= 1
	}, "crashy worker to have restarted at least once before hammering GetAllStates")

	const numReaders = 8
	const duration = 1500 * time.Millisecond

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				views := sup.GetAllStates()
				for _, v := range views {
					// Touch every exported field a UI render pass would
					// read, so -race has something to catch if a field is
					// ever read without going through the Snapshot() copy.
					_ = v.Config.ID
					_ = v.Status
					_ = v.PID
					_ = v.StartedAt
					_ = v.StoppedAt
					_ = v.Restarts
					_ = v.LastExitCode
					_ = v.LastError
					_ = v.CPUPercent
					_ = v.MemoryBytes
					for _, l := range v.Logs {
						_ = l.Text
					}
					for _, h := range v.CPUHistory {
						_ = h
					}
					for _, m := range v.MemHistory {
						_ = m
					}
					total, rate, succ, fail := v.GetRunStats()
					_ = total
					_ = rate
					_ = succ
					_ = fail
				}

				// Also exercise the single-sidecar lookup path concurrently.
				if _, ok := sup.GetState("chatty"); !ok {
					t.Errorf("GetState(chatty) unexpectedly missing mid-run")
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	// Sanity: the supervisor should still be in a coherent, readable state
	// after the hammering (not e.g. deadlocked -- GetAllStates would hang
	// above if it were, and the test would time out instead of reaching
	// here).
	final := sup.GetAllStates()
	if len(final) != 2 {
		t.Fatalf("expected 2 sidecars in final snapshot, got %d", len(final))
	}
}

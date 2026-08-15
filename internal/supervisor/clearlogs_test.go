package supervisor

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"agytop/internal/config"
)

// Verifies the live-process interaction that reading the code alone cannot
// settle: clearing the in-memory buffer while TailFile is actively appending
// must not resurrect already-tailed lines. TailFile seeks from EOF and only
// pushes forward, so a clear should be durable.
//
// The assertion is on line identity rather than entry counts: the daemon emits
// monotonically numbered lines, so "every line present after the clear is
// newer than every line present before it" holds regardless of how fast the
// machine running the test happens to be.
func TestClearLogsDoesNotResurrectTailedLines(t *testing.T) {
	dir := t.TempDir()

	cfgs := []config.SidecarConfig{{
		ID:            "chatty",
		DisplayName:   "Chatty Daemon",
		Command:       "bash",
		Args:          []string{"-c", "for i in $(seq 1 400); do echo line-$i; sleep 0.01; done"},
		WorkingDir:    dir,
		Scope:         "custom",
		RestartPolicy: config.RestartNever,
	}}

	sup := NewSupervisorWithRegistry(cfgs, NewRegistryAt(t.TempDir()))
	// Shutdown() only closes the supervisor's stop channel -- it deliberately
	// leaves detached (Setsid) daemons running. stopAndWait stops them and
	// blocks until they are gone, so TempDir cleanup cannot race the writer.
	t.Cleanup(func() { stopAndWait(t, sup, "chatty") })

	if err := sup.Start("chatty"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		st, ok := sup.GetState("chatty")
		return ok && maxLineNo(st.Logs) >= 25
	}, "logs to accumulate before clearing")

	before, _ := sup.GetState("chatty")
	highWater := maxLineNo(before.Logs)
	t.Logf("%d entries before clear, highest line-%d", len(before.Logs), highWater)

	if err := sup.ClearLogs("chatty"); err != nil {
		t.Fatalf("ClearLogs failed: %v", err)
	}

	// A couple of entries may race in from the tailer between the clear and
	// this read; what must not happen is the old lines returning en masse.
	justAfter, _ := sup.GetState("chatty")
	if len(justAfter.Logs) > 5 {
		t.Fatalf("expected a near-empty buffer right after clear, got %d entries", len(justAfter.Logs))
	}

	// Let the daemon keep writing, then confirm every surviving line is newer
	// than everything that existed before the clear.
	time.Sleep(500 * time.Millisecond)

	after, _ := sup.GetState("chatty")
	t.Logf("%d entries 500ms after clear, lines %d..%d", len(after.Logs), minLineNo(after.Logs), maxLineNo(after.Logs))

	if len(after.Logs) == 0 {
		t.Fatal("expected new lines after the clear; the tailer may have stalled")
	}

	for _, e := range after.Logs {
		n, ok := lineNo(e)
		if !ok {
			continue // supervisor/system entries carry no line number
		}
		if n <= highWater {
			t.Errorf("line-%d reappeared after the clear (high-water mark was line-%d)", n, highWater)
		}
	}
}

// lineNo extracts N from a "line-N" stdout entry.
func lineNo(e LogEntry) (int, bool) {
	if e.Source != SourceStdout {
		return 0, false
	}
	_, num, found := strings.Cut(strings.TrimSpace(e.Text), "line-")
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(num)
	return n, err == nil
}

func maxLineNo(entries []LogEntry) int {
	max := 0
	for _, e := range entries {
		if n, ok := lineNo(e); ok && n > max {
			max = n
		}
	}
	return max
}

func minLineNo(entries []LogEntry) int {
	min := 0
	for _, e := range entries {
		if n, ok := lineNo(e); ok && (min == 0 || n < min) {
			min = n
		}
	}
	return min
}

// newRegistryDir returns a temp directory for a Registry that is removed with
// retries instead of by t.TempDir().
//
// A restarting sidecar creates its log file in the registry directory on every
// respawn. runProcessLoop only checks stopChan at the top of its loop, so a
// Stop() landing just after that check still allows one more iteration to
// create a file -- a file can therefore appear *after* Stop returns.
// t.TempDir()'s RemoveAll is not retried and fails the whole test when it
// loses that race with "directory not empty". Retrying tolerates the final
// straggler. Use this for any test that starts a sidecar with a restart
// policy; plain t.TempDir() is fine elsewhere.
func newRegistryDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "agytop-registry-")
	if err != nil {
		t.Fatalf("failed to create registry dir: %v", err)
	}

	// Callers register their shutdown cleanup after this one, so t.Cleanup's
	// LIFO ordering runs this removal only once the processes are stopped.
	t.Cleanup(func() {
		for i := 0; i < 100; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("warning: could not remove registry dir %s", dir)
	})

	return dir
}

// stopAndWait shuts a supervisor down and blocks until its detached children
// are actually gone.
//
// ShutdownAndStopAll returns as soon as it has signalled each process, not
// once they have exited. Those children hold their log files open under the
// registry directory, so if that directory is a t.TempDir() the removal can
// race a still-dying writer and fail with "directory not empty". It only
// showed up on the macOS CI legs -- Linux happened to win the race locally
// every time -- so waiting on the PIDs is what actually makes it
// deterministic rather than lucky.
func stopAndWait(t *testing.T, sup *Supervisor, ids ...string) {
	t.Helper()

	// Read PIDs at cleanup time, not registration time: with a restart policy
	// in play the live PID changes over the life of the test.
	pids := make([]int, 0, len(ids))
	for _, id := range ids {
		if st, ok := sup.GetState(id); ok && st.PID > 0 {
			pids = append(pids, st.PID)
		}
	}

	sup.ShutdownAndStopAll()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		anyAlive := false
		for _, pid := range pids {
			if IsPIDAlive(pid) {
				anyAlive = true
				break
			}
		}
		if !anyAlive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("child processes still alive 5s after shutdown: %v", pids)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

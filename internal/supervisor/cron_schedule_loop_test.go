package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agytop/internal/config"
)

// fakeCronClock is a controllable, step-gated time source for
// runBuiltinScheduleLoop. Now reports a mutable virtual "current time".
// After, like time.After, returns immediately (never blocks -- it must not,
// since production code calls it inline as a select case expression) but the
// returned channel only receives a value once the test calls Release. This
// lets a schedule that is genuinely hours or days out (e.g. "0 0 * * *") be
// driven through many real firings in milliseconds without changing anything
// about the cron math itself -- the loop still computes exactly the same
// Next()/duration it would against a real clock, it just doesn't wait in
// real time for that duration to elapse -- while keeping each step
// deterministic: because runBuiltinScheduleLoop always records
// NextScheduleRun *before* calling After, a test can assert on that
// intermediate value with no race against a free-running loop, and because
// After never blocks, the surrounding select stays responsive to
// state.stopChan exactly as it does against the real clock (Stop() during a
// pending step still interrupts the loop immediately rather than deadlocking
// on a blocked After call).
type fakeCronClock struct {
	mu      sync.Mutex
	now     time.Time
	pending chan time.Time
	dur     time.Duration
	arrived chan struct{} // non-blocking "a new After call registered" signal
}

func newFakeCronClock(start time.Time) *fakeCronClock {
	return &fakeCronClock{now: start, arrived: make(chan struct{}, 1)}
}

func (f *fakeCronClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeCronClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	ch := make(chan time.Time, 1)
	f.pending = ch
	f.dur = d
	f.mu.Unlock()

	select {
	case f.arrived <- struct{}{}:
	default:
	}
	return ch
}

// Release waits for a pending After call to have registered, then advances
// the virtual clock by that call's requested duration and delivers on its
// channel -- unblocking whichever select in runBuiltinScheduleLoop is
// waiting on it.
func (f *fakeCronClock) Release(t *testing.T) {
	t.Helper()
	select {
	case <-f.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("fakeCronClock.Release timed out waiting for the scheduler loop to call After")
	}

	f.mu.Lock()
	ch := f.pending
	f.pending = nil
	f.now = f.now.Add(f.dur)
	fired := f.now
	f.mu.Unlock()

	if ch == nil {
		t.Fatal("fakeCronClock.Release: no pending After call")
		return
	}
	ch <- fired
}

// TestRunBuiltinScheduleLoopFiresOnParsedSchedule proves runBuiltinScheduleLoop
// actually consults the parsed cron expression -- not a fixed interval -- by
// using an expression ("0 0 * * *", real-world nightly) that would never
// fire within any reasonable test timeout on a real clock. With the fake
// clock seam it fires multiple times almost instantly, and NextScheduleRun
// is asserted to always land exactly on a midnight boundary (proving it
// reflects the parsed schedule, not "started_at + fixed offset").
func TestRunBuiltinScheduleLoopFiresOnParsedSchedule(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nightly.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho tick\nexit 0\n"), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	cfg := config.SidecarConfig{
		ID:          "nightly",
		DisplayName: "Nightly Job",
		Builtin:     "schedule",
		Command:     scriptPath,
		Schedule:    "0 0 * * *", // real nightly cadence -- must NOT need real wall-clock days to test
		WorkingDir:  tmpDir,
	}

	sup := NewSupervisorWithRegistry([]config.SidecarConfig{cfg}, NewRegistryAt(newRegistryDir(t)))
	t.Cleanup(func() { stopAndWait(t, sup, "nightly") })

	fake := newFakeCronClock(utc(2024, 1, 1, 12, 0, 0)) // noon -- next midnight is 12h away
	sup.cronNow = fake.Now
	sup.cronAfter = fake.After

	if err := sup.Start("nightly"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// The loop sets NextScheduleRun before ever calling After, and that
	// first After call blocks on the fake clock's step gate -- so this is
	// deterministic, not a race against a free-running loop. The very first
	// value must be the next midnight after noon on 2024-01-01, i.e.
	// 2024-01-02T00:00:00 -- not "started_at + 30s" (the old bug) and not
	// "started_at + 1 minute" (the NewSidecarState placeholder this task
	// also removes).
	wantMidnights := []time.Time{
		utc(2024, 1, 2, 0, 0, 0),
		utc(2024, 1, 3, 0, 0, 0),
		utc(2024, 1, 4, 0, 0, 0),
	}

	for i, want := range wantMidnights {
		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("nightly")
			return ok && st.NextScheduleRun.Equal(want)
		}, "NextScheduleRun to reach midnight boundary")

		st, _ := sup.GetState("nightly")
		if !st.NextScheduleRun.Equal(want) {
			t.Fatalf("midnight #%d: NextScheduleRun = %v, want %v", i+1, st.NextScheduleRun, want)
		}

		// Let this fire (recording a run) and advance to the next boundary.
		fake.Release(t)

		waitFor(t, 5*time.Second, func() bool {
			st, ok := sup.GetState("nightly")
			return ok && len(st.RunHistory) >= i+1
		}, "the fired run to be recorded in RunHistory")
	}

	// Stop before releasing any further step: with no real delay between
	// fake-clock firings the loop would otherwise keep spawning
	// "nightly.sh" as fast as the OS can schedule it for as long as the
	// test process is alive.
	if err := sup.Stop("nightly"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	state, _ := sup.GetState("nightly")
	if len(state.RunHistory) < len(wantMidnights) {
		t.Fatalf("expected at least %d recorded runs, got %d", len(wantMidnights), len(state.RunHistory))
	}
	for i, r := range state.RunHistory {
		if r.Trigger != TriggerCron {
			t.Errorf("run %d: trigger = %v, want %v", i, r.Trigger, TriggerCron)
		}
	}
}

// TestRunBuiltinScheduleLoopInvalidExpression asserts the chosen behavior for
// a broken/empty schedule: it must NOT silently fall back to some default
// cadence (that would just be the original bug in a new costume). Instead
// the sidecar should end up visibly broken -- FAILED status, a non-empty
// LastError naming the problem, a supervisor log line about it, and no
// NextScheduleRun -- so a bad sidecar.json is obvious in the TUI rather than
// quietly firing on the wrong schedule (or never).
func TestRunBuiltinScheduleLoopInvalidExpression(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
	}{
		{"empty expression", ""},
		{"malformed expression", "not a cron expr"},
		{"out of range field", "99 * * * *"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.SidecarConfig{
				ID:          "broken-cron",
				DisplayName: "Broken Cron",
				Builtin:     "schedule",
				Schedule:    tc.schedule,
			}

			sup := NewSupervisorWithRegistry([]config.SidecarConfig{cfg}, NewRegistryAt(newRegistryDir(t)))
			t.Cleanup(sup.ShutdownAndStopAll)

			if err := sup.Start("broken-cron"); err != nil {
				t.Fatalf("Start failed: %v", err)
			}

			waitFor(t, 5*time.Second, func() bool {
				st, ok := sup.GetState("broken-cron")
				return ok && st.Status == StatusFailed
			}, "scheduler to surface the invalid expression as FAILED")

			state, _ := sup.GetState("broken-cron")
			if state.LastError == "" {
				t.Error("expected a non-empty LastError describing the invalid schedule")
			}
			if !state.NextScheduleRun.IsZero() {
				t.Errorf("expected no NextScheduleRun for an invalid schedule, got %v", state.NextScheduleRun)
			}

			foundLog := false
			for _, l := range state.Logs {
				if l.Source == SourceSupervisor && strings.Contains(strings.ToLower(l.Text), "invalid") {
					foundLog = true
					break
				}
			}
			if !foundLog {
				t.Error("expected a supervisor log entry mentioning the invalid schedule")
			}
		})
	}
}

// TestNewSidecarStateComputesRealNextScheduleRun asserts NewSidecarState's
// initial NextScheduleRun estimate for a valid schedule is the genuine
// parsed next-fire time (within a generous tolerance for the real-time call
// to time.Now inside it), not the old "time.Now().Add(1 * time.Minute)"
// placeholder, and that an invalid expression leaves it zero rather than
// guessing.
func TestNewSidecarStateComputesRealNextScheduleRun(t *testing.T) {
	before := time.Now()
	state := NewSidecarState(config.SidecarConfig{ID: "x", Schedule: "*/15 * * * *"})
	after := time.Now()

	if state.NextScheduleRun.IsZero() {
		t.Fatal("expected a non-zero NextScheduleRun for a valid schedule")
	}

	sched, err := ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatalf("ParseCron failed: %v", err)
	}
	wantEarliest := sched.Next(before)
	wantLatest := sched.Next(after)
	if state.NextScheduleRun.Before(wantEarliest) || state.NextScheduleRun.After(wantLatest.Add(time.Minute)) {
		t.Errorf("NextScheduleRun = %v, want within [%v, %v] (the real parsed next-fire window, not a fixed +1m placeholder)",
			state.NextScheduleRun, wantEarliest, wantLatest)
	}

	invalid := NewSidecarState(config.SidecarConfig{ID: "y", Schedule: "not a cron expr"})
	if !invalid.NextScheduleRun.IsZero() {
		t.Errorf("expected zero NextScheduleRun for an invalid schedule, got %v", invalid.NextScheduleRun)
	}
}

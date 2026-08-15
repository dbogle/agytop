package ui

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"agytop/internal/config"
	"agytop/internal/supervisor"
)

// ---------------------------------------------------------------------
// Headless tea.Program harness
//
// teatest (github.com/charmbracelet/x/exp/teatest) was evaluated and
// rejected: `go get`-ing it rewrites the go directive from 1.22.6 to 1.24.0,
// which breaks both CI Go matrix legs, and it drags bubbletea 0.25.0 -> 1.3.5
// and lipgloss 0.9.1 -> 1.1.0 along with it -- major-version jumps of the
// libraries the whole UI is built on, from a module with no semver tags to
// pin a compatible older version against.
//
// Everything teatest wraps for our purposes is already in bubbletea v0.25.0:
// tea.WithInput/tea.WithOutput as program options, and Program.Run/Send/Quit
// as the drive/observe surface. This file builds the same shape directly
// against the real tea.Program, with no new dependency.
// ---------------------------------------------------------------------

// syncBuffer is a concurrency-safe io.Writer + fmt.Stringer. The renderer
// goroutine inside tea.Program writes rendered frames to it continuously
// while the test goroutine polls its contents; without its own lock here,
// `go test -race` fires on essentially every test in this file.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// setStates lets a headless test mutate the fake supervisor's state list
// after the program has already started, so the 200ms tickMsg poll has
// something new to observe without any key being pressed. Every other
// fakeSupervisor method (fake_supervisor_test.go) is fixed at construction
// time -- this setter is the one genuinely new piece of surface the Phase 3
// harness needs, added here rather than touching the existing fake.
func (f *fakeSupervisor) setStates(states []supervisor.StateView) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = states
}

// newHeadlessProgram builds a real tea.Program wired to an in-memory input
// (never written to -- every test here drives via p.Send, not raw bytes) and
// a concurrency-safe output buffer, with no alt-screen, no signal handler,
// and no TTY of any kind involved.
func newHeadlessProgram(sup supervisorAPI) (*tea.Program, *syncBuffer) {
	out := &syncBuffer{}
	m := NewModel(sup)
	p := tea.NewProgram(m,
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
		tea.WithoutCatchPanics(),
	)
	return p, out
}

// runHeadless starts p.Run() on its own goroutine -- Run blocks until the
// program quits, so calling it inline would hang the test -- and returns a
// channel that receives its terminal error exactly once.
func runHeadless(p *tea.Program) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	return done
}

// waitForProgramExit blocks on done, failing loudly instead of hanging CI if
// the program does not shut down within timeout.
func waitForProgramExit(t *testing.T, done <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatalf("program did not exit within %v", timeout)
		return nil
	}
}

// waitForOutput polls out for a frame satisfying want, failing loudly rather
// than hanging if it never appears within timeout. Bubble Tea's renderer
// redraws asynchronously, so tests must poll rather than assert immediately
// after sending a message.
func waitForOutput(t *testing.T, out *syncBuffer, timeout time.Duration, want func(frame string) bool, desc string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = out.String()
		if want(last) {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s; last frame:\n%s", timeout, desc, visible(last))
	return ""
}

// demoProgramStates gives the headless program something recognizable to
// render: one running daemon and one scheduled cron-style task.
func demoProgramStates() []supervisor.StateView {
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
				Schedule:    "0 * * * *",
			},
			Status: supervisor.StatusScheduled,
		},
	}
}

const headlessTimeout = 5 * time.Second

// ---------------------------------------------------------------------
// 1. Program starts, renders the sidecar list, quits cleanly on 'q'.
// ---------------------------------------------------------------------

func TestHeadlessProgram_StartsRendersAndQuits(t *testing.T) {
	fake := newFakeSupervisor(demoProgramStates()...)
	p, out := newHeadlessProgram(fake)

	done := runHeadless(p)

	// Model.View() shows only the loading line until it has a WindowSizeMsg,
	// and nothing sends one automatically: handleResize only queries a real
	// terminal size when the output is a *os.File TTY, which our buffer is
	// not. The harness must supply it itself, exactly as a real terminal's
	// initial SIGWINCH would.
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 40})

	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		v := visible(frame)
		return strings.Contains(v, "Alpha Daemon") && strings.Contains(v, "Beta Cron Job")
	}, "initial frame containing the sidecar list")

	p.Send(keyRunes("q"))

	if err := waitForProgramExit(t, done, headlessTimeout); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	if calls := fake.shutdownCalls; calls != 1 {
		t.Errorf("supervisor.Shutdown() called %d times, want 1", calls)
	}
}

// ---------------------------------------------------------------------
// 2. Navigate, open a modal, close it, quit -- assert on rendered content.
// ---------------------------------------------------------------------

func TestHeadlessProgram_NavigateModalAndQuit(t *testing.T) {
	fake := newFakeSupervisor(demoProgramStates()...)
	p, out := newHeadlessProgram(fake)

	done := runHeadless(p)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 40})

	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		return strings.Contains(visible(frame), "Alpha Daemon")
	}, "initial frame")

	// Navigate down to the second sidecar (cursor starts at 0 / Alpha).
	p.Send(keyRunes("j"))
	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		v := visible(frame)
		return strings.Contains(v, "Beta Cron Job") && strings.Contains(v, "BUILTIN: schedule")
	}, "inspector focused on Beta Cron Job after navigating down")

	// Open the JSON config modal ('v') and confirm modal content replaces the
	// normal three-pane layout.
	p.Send(keyRunes("v"))
	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		v := visible(frame)
		return strings.Contains(v, "beta-cron") && strings.Contains(v, "\"id\"")
	}, "config modal showing the raw JSON for beta-cron")

	// Close it and confirm the normal layout (footer hints) is back.
	p.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		return strings.Contains(visible(frame), "DRY RUN") // footer hint text
	}, "main layout restored after closing config modal")

	p.Send(keyRunes("q"))
	if err := waitForProgramExit(t, done, headlessTimeout); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------
// 3. The 200ms tickMsg cadence: state changed on the fake supervisor
//    becomes visible in a later frame without any key being pressed.
// ---------------------------------------------------------------------

func TestHeadlessProgram_TickPicksUpStateChangeWithoutInput(t *testing.T) {
	fake := newFakeSupervisor(demoProgramStates()...)
	p, out := newHeadlessProgram(fake)

	done := runHeadless(p)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 40})

	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		v := visible(frame)
		return strings.Contains(v, "Alpha Daemon") && !strings.Contains(v, "Gamma Newcomer")
	}, "initial frame without the not-yet-added sidecar")

	// Mutate the fake's backing state directly -- no key sent, no Update
	// call made by the test. Only Model's own 200ms tickMsg loop (Init/
	// Update in model.go) can be what picks this up.
	next := append(append([]supervisor.StateView{}, demoProgramStates()...), supervisor.StateView{
		Config: config.SidecarConfig{
			ID:          "gamma",
			DisplayName: "Gamma Newcomer",
			Scope:       "workspace",
			Command:     "bash",
		},
		Status: supervisor.StatusStopped,
	})
	fake.setStates(next)

	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		return strings.Contains(visible(frame), "Gamma Newcomer")
	}, "a later frame reflecting the fake supervisor's state change, driven only by tickMsg polling")

	p.Send(keyRunes("q"))
	if err := waitForProgramExit(t, done, headlessTimeout); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------
// 4. Clean shutdown: Run() returns without error, no goroutine leak.
// ---------------------------------------------------------------------

func TestHeadlessProgram_CleanShutdownNoLeak(t *testing.T) {
	fake := newFakeSupervisor(demoProgramStates()...)
	p, out := newHeadlessProgram(fake)

	before := countGoroutines()

	done := runHeadless(p)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
	waitForOutput(t, out, headlessTimeout, func(frame string) bool {
		return strings.Contains(visible(frame), "Alpha Daemon")
	}, "initial frame")

	p.Send(keyRunes("q"))

	err := waitForProgramExit(t, done, headlessTimeout)
	if err != nil {
		t.Fatalf("Run() returned error on clean shutdown: %v", err)
	}

	// Give any trailing internal goroutines (renderer, cancel-reader
	// teardown) a moment to actually finish past the point Run() returned,
	// then compare against the baseline rather than asserting an exact
	// count -- the Go runtime's own bookkeeping goroutines are not stable.
	waitFor(t, headlessTimeout, func() bool {
		return countGoroutines() <= before+1 // small slack for test-runner noise
	}, "goroutine count to settle back near baseline")
}

// countGoroutines is a coarse leak signal, not a precise measurement -- the
// Go runtime's own housekeeping goroutines fluctuate independent of this
// test, so callers must compare against a baseline with slack rather than
// asserting an exact count.
func countGoroutines() int {
	return runtime.NumGoroutine()
}

// waitFor is a small local polling helper (mirrors the pattern already used
// in internal/supervisor/clearlogs_test.go) kept private to this file since
// it is generic across the fixed-timeout assertions above that don't need
// waitForOutput's frame-matching signature.
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

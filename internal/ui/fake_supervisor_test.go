package ui

import (
	"sync"

	"agytop/internal/supervisor"
)

// fakeCall records a single method invocation against fakeSupervisor, in the
// order it happened, so tests can assert both "was X called" and "was X
// called with the right id" without needing real processes.
type fakeCall struct {
	method string
	id     string
}

// fakeSupervisor implements supervisorAPI entirely in memory. It exists so
// the Model.Update action-key paths (s/x/r/d/t/c) can be exercised without
// spawning anything -- the whole reason the supervisorAPI seam exists.
type fakeSupervisor struct {
	mu sync.Mutex

	calls []fakeCall

	states []supervisor.StateView

	// errs maps a method name ("Start", "Stop", "Restart", "DryRun",
	// "TriggerScheduled", "ClearLogs") to an error that method should return
	// next. Tests inject failures here.
	errs map[string]error

	// dryRunResult is returned by DryRun on success (err == nil).
	dryRunResult *supervisor.DryRunResult

	shutdownCalls int
}

func newFakeSupervisor(states ...supervisor.StateView) *fakeSupervisor {
	return &fakeSupervisor{
		states: states,
		errs:   make(map[string]error),
		dryRunResult: &supervisor.DryRunResult{
			Success:        true,
			ValidationMsgs: []string{"✓ fake probe"},
		},
	}
}

func (f *fakeSupervisor) record(method, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{method: method, id: id})
}

// callsTo returns the ids passed to every recorded call of the given method,
// in call order.
func (f *fakeSupervisor) callsTo(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for _, c := range f.calls {
		if c.method == method {
			ids = append(ids, c.id)
		}
	}
	return ids
}

func (f *fakeSupervisor) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[method] = err
}

func (f *fakeSupervisor) errFor(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[method]
}

func (f *fakeSupervisor) Start(id string) error {
	f.record("Start", id)
	return f.errFor("Start")
}

func (f *fakeSupervisor) Stop(id string) error {
	f.record("Stop", id)
	return f.errFor("Stop")
}

func (f *fakeSupervisor) Restart(id string) error {
	f.record("Restart", id)
	return f.errFor("Restart")
}

func (f *fakeSupervisor) DryRun(id string) (*supervisor.DryRunResult, error) {
	f.record("DryRun", id)
	if err := f.errFor("DryRun"); err != nil {
		return nil, err
	}
	return f.dryRunResult, nil
}

func (f *fakeSupervisor) TriggerScheduled(id string) error {
	f.record("TriggerScheduled", id)
	return f.errFor("TriggerScheduled")
}

func (f *fakeSupervisor) ClearLogs(id string) error {
	f.record("ClearLogs", id)
	return f.errFor("ClearLogs")
}

func (f *fakeSupervisor) GetAllStates() []supervisor.StateView {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]supervisor.StateView, len(f.states))
	copy(out, f.states)
	return out
}

func (f *fakeSupervisor) GetState(id string) (supervisor.StateView, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.states {
		if s.Config.ID == id {
			return s, true
		}
	}
	return supervisor.StateView{}, false
}

func (f *fakeSupervisor) Shutdown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalls++
}

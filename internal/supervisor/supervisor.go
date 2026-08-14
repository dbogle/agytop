package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"agytop/internal/config"
)

// Supervisor coordinates multiple sidecar lifecycles
type Supervisor struct {
	mu       sync.RWMutex
	sidecars map[string]*SidecarState
	order    []string
	registry *Registry
	stopChan chan struct{}
}

// NewSupervisor creates a supervisor from sidecar configurations
func NewSupervisor(configs []config.SidecarConfig) *Supervisor {
	return NewSupervisorWithRegistry(configs, NewRegistry())
}

// NewSupervisorWithRegistry creates a supervisor backed by a caller-provided
// registry. Exposed primarily so tests can inject a temp-dir-backed registry
// instead of reading/writing the user's real ~/.agytop/state.json.
func NewSupervisorWithRegistry(configs []config.SidecarConfig, registry *Registry) *Supervisor {
	sup := &Supervisor{
		sidecars: make(map[string]*SidecarState),
		order:    make([]string, 0, len(configs)),
		registry: registry,
		stopChan: make(chan struct{}),
	}

	for _, cfg := range configs {
		sup.AddOrUpdate(cfg)
	}

	// Re-attach to any sidecars running detached in the background
	sup.reAttachRunningSidecars()

	// Start background metrics poller
	go sup.metricsLoop()

	return sup
}

// reAttachRunningSidecars inspects state.json and reconnects to live detached
// PIDs. Records with no matching currently-discovered sidecar are surfaced as
// orphaned entries (rather than silently dropped) so a live process is never
// left both invisible and unstoppable; genuinely dead entries are pruned.
func (s *Supervisor) reAttachRunningSidecars() {
	persisted, err := s.registry.Load()
	if err != nil || len(persisted) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, record := range persisted {
		if record.PID <= 0 || !IsPIDAlive(record.PID) {
			// Stale entry in registry, clean up
			_ = s.registry.UpdateState(record, true)
			continue
		}

		state, ok := s.sidecars[id]
		if !ok {
			// No sidecar.json currently matches this ID (renamed/removed or
			// discovery scope changed), but its detached process is still
			// alive. Synthesize a minimal entry so it stays visible and
			// stoppable instead of leaking silently in state.json.
			cfg := config.SidecarConfig{
				ID:            record.ID,
				DisplayName:   fmt.Sprintf("%s (orphaned)", record.ID),
				Description:   "No matching sidecar.json found; reattached from a previous session.",
				Command:       record.Command,
				Args:          record.Args,
				Directory:     record.WorkingDir,
				Schedule:      record.Schedule,
				RestartPolicy: config.RestartNever,
			}
			state = NewSidecarState(cfg)
			s.sidecars[id] = state
			s.order = append(s.order, id)
		}

		state.mu.Lock()
		state.Status = StatusRunning
		state.PID = record.PID
		state.StartedAt = record.StartedAt
		state.stopChan = make(chan struct{})
		stopChan := state.stopChan
		state.mu.Unlock()

		state.AddLog(SourceSupervisor, fmt.Sprintf("Re-attached to running detached sidecar (PID %d).", record.PID))

		// Resume live log tailing
		if record.LogFile != "" {
			go TailFile(record.LogFile, state, stopChan)
		}

		// We don't own this process's *exec.Cmd (it wasn't started by
		// this instance), so cmd.Wait() isn't available; poll liveness
		// instead so a crash after reattach is still detected and
		// restart policy applied.
		go s.watchReattachedProcess(state, record.PID, stopChan)
	}
}

// watchReattachedProcess polls a reattached process's liveness and, once it
// exits, applies the sidecar's restart policy — mirroring what runProcessLoop
// does for processes this instance launched directly. The exit code of a
// reattached process is unknowable (we never had a handle on it), so only
// the "always" restart policy is honored here.
func (s *Supervisor) watchReattachedProcess(state *SidecarState, pid int, stopChan chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	processExited := false
	for !processExited {
		select {
		case <-stopChan:
			// Stop() already handled termination, state, and registry cleanup.
			return
		case <-ticker.C:
			processExited = !IsPIDAlive(pid)
		}
	}

	cfg := state.Config
	_ = s.registry.UpdateState(PersistedState{ID: cfg.ID}, true)

	state.mu.Lock()
	state.StoppedAt = time.Now()
	state.PID = 0
	state.CPUPercent = 0
	state.MemoryBytes = 0
	state.mu.Unlock()

	state.AddLog(SourceSupervisor, fmt.Sprintf("Reattached process (PID %d) exited while unmonitored (exit code unknown).", pid))

	if cfg.RestartPolicy == config.RestartAlways {
		state.mu.Lock()
		state.Restarts++
		state.Status = StatusBackoff
		state.mu.Unlock()
		state.AddLog(SourceSupervisor, "Restarting per restart policy 'always'...")
		go s.runProcessLoop(state)
		return
	}

	state.mu.Lock()
	state.Status = StatusStopped
	state.mu.Unlock()
}

// AddOrUpdate registers or updates a sidecar configuration
func (s *Supervisor) AddOrUpdate(cfg config.SidecarConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.sidecars[cfg.ID]
	if !exists {
		state = NewSidecarState(cfg)
		s.sidecars[cfg.ID] = state
		s.order = append(s.order, cfg.ID)
	} else {
		state.mu.Lock()
		state.Config = cfg
		state.mu.Unlock()
	}
}

// GetAllStates returns snapshots of all sidecars in stable order
func (s *Supervisor) GetAllStates() []StateView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]StateView, 0, len(s.order))
	for _, id := range s.order {
		if state, ok := s.sidecars[id]; ok {
			res = append(res, state.Snapshot())
		}
	}
	return res
}

// GetState returns a snapshot of a single sidecar
func (s *Supervisor) GetState(id string) (StateView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.sidecars[id]
	if !ok {
		return StateView{}, false
	}
	return state.Snapshot(), true
}

// Start launches a sidecar process or scheduler
func (s *Supervisor) Start(id string) error {
	s.mu.RLock()
	state, ok := s.sidecars[id]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("sidecar %s not found", id)
	}

	state.mu.Lock()
	if state.Status == StatusRunning || state.Status == StatusScheduled || state.Status == StatusExecuting {
		state.mu.Unlock()
		return nil
	}

	if state.Config.Builtin == "schedule" {
		state.Status = StatusScheduled
	} else {
		state.Status = StatusRunning
	}
	state.LastError = ""
	state.stopChan = make(chan struct{})
	state.mu.Unlock()

	state.AddLog(SourceSupervisor, fmt.Sprintf("Starting sidecar '%s'...", state.Config.GetDisplayName()))

	// Handle builtin vs command
	if state.Config.Builtin == "schedule" {
		go s.runBuiltinScheduleLoop(state)
		return nil
	}

	go s.runProcessLoop(state)
	return nil
}

// Stop terminates a sidecar
func (s *Supervisor) Stop(id string) error {
	s.mu.RLock()
	state, ok := s.sidecars[id]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("sidecar %s not found", id)
	}

	state.mu.Lock()
	pid := state.PID
	cmd := state.cmd
	if state.Status != StatusRunning && state.Status != StatusBackoff && state.Status != StatusScheduled && state.Status != StatusExecuting && pid == 0 {
		state.mu.Unlock()
		return nil
	}

	close(state.stopChan)
	if state.cancelFunc != nil {
		state.cancelFunc()
	}
	state.mu.Unlock()

	state.AddLog(SourceSupervisor, fmt.Sprintf("Stopping sidecar (PID %d)...", pid))
	if pid > 0 {
		_ = TerminatePID(pid)
	}
	if cmd != nil {
		_ = killProcessGroup(cmd)
	}
	_ = s.registry.UpdateState(PersistedState{ID: id}, true)

	state.mu.Lock()
	state.Status = StatusStopped
	state.StoppedAt = time.Now()
	state.PID = 0
	state.CPUPercent = 0
	state.MemoryBytes = 0
	state.cmd = nil
	state.mu.Unlock()

	state.AddLog(SourceSupervisor, "Process stopped.")
	return nil
}

// Restart stops and starts the sidecar
func (s *Supervisor) Restart(id string) error {
	if err := s.Stop(id); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return s.Start(id)
}

// DryRun performs a validation and non-destructive dry-run probe of the sidecar
func (s *Supervisor) DryRun(id string) (*DryRunResult, error) {
	s.mu.RLock()
	state, ok := s.sidecars[id]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("sidecar %s not found", id)
	}

	state.mu.RLock()
	cfg := state.Config
	state.mu.RUnlock()

	startTime := time.Now()
	result := &DryRunResult{
		SidecarID:      id,
		Timestamp:      startTime,
		Success:        true,
		Command:        cfg.Command,
		Args:           cfg.Args,
		WorkingDir:     cfg.EffectiveWorkingDir(),
		Env:            make(map[string]string),
		Logs:           make([]LogEntry, 0),
		ValidationMsgs: make([]string, 0),
	}

	// Copy configured env
	for k, v := range cfg.Env {
		result.Env[k] = v
	}
	// Injected dry-run flags
	result.Env["AGY_DRY_RUN"] = "1"
	result.Env["DRY_RUN"] = "true"
	result.Env["ANTIGRAVITY_SIDECAR_DRY_RUN"] = "1"

	addValidation := func(msg string, pass bool) {
		prefix := "✓"
		if !pass {
			prefix = "✗"
			result.Success = false
		}
		result.ValidationMsgs = append(result.ValidationMsgs, fmt.Sprintf("%s %s", prefix, msg))
	}

	// 1. Validate Working Directory
	workDir := cfg.EffectiveWorkingDir()
	if info, err := os.Stat(workDir); err != nil || !info.IsDir() {
		addValidation(fmt.Sprintf("Working Directory '%s' does not exist or is not a directory", workDir), false)
	} else {
		addValidation(fmt.Sprintf("Working Directory '%s' exists and is accessible", workDir), true)
	}

	// 2. Validate Builtin Schedule if applicable
	if cfg.Builtin == "schedule" {
		schedExpr := cfg.Schedule
		if schedExpr == "" {
			schedExpr = "* * * * *"
		}
		addValidation(fmt.Sprintf("Builtin 'schedule' detected with interval/cron expression: '%s'", schedExpr), true)

		// Generate simulated trigger times
		result.NextSchedules = []string{
			startTime.Add(1 * time.Minute).Format("15:04:05 (in 1m)"),
			startTime.Add(2 * time.Minute).Format("15:04:05 (in 2m)"),
			startTime.Add(3 * time.Minute).Format("15:04:05 (in 3m)"),
			startTime.Add(4 * time.Minute).Format("15:04:05 (in 4m)"),
			startTime.Add(5 * time.Minute).Format("15:04:05 (in 5m)"),
		}
	}

	// 3. Validate Executable Command
	if cfg.Command != "" {
		resolvedPath := ""
		if filepath.IsAbs(cfg.Command) {
			resolvedPath = cfg.Command
		} else if strings.Contains(cfg.Command, string(filepath.Separator)) {
			resolvedPath = filepath.Join(workDir, cfg.Command)
		} else {
			if lp, err := exec.LookPath(cfg.Command); err == nil {
				resolvedPath = lp
			}
		}

		if resolvedPath != "" {
			if info, err := os.Stat(resolvedPath); err == nil && !info.IsDir() {
				result.ExecutablePath = resolvedPath
				addValidation(fmt.Sprintf("Executable resolved at: %s", resolvedPath), true)
			} else {
				addValidation(fmt.Sprintf("Command '%s' path was not found or not executable", resolvedPath), false)
			}
		} else {
			addValidation(fmt.Sprintf("Command '%s' not found in PATH or working directory", cfg.Command), false)
		}

		// Execute dry-run invocation with timeout and simulated flags
		if result.ExecutablePath != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			dryRunArgs := append([]string{}, cfg.Args...)
			// If arguments don't already include dry-run, we test invocation
			dryRunCmd := exec.CommandContext(ctx, result.ExecutablePath, dryRunArgs...)
			dryRunCmd.Dir = workDir
			dryRunCmd.Env = os.Environ()
			for k, v := range result.Env {
				dryRunCmd.Env = append(dryRunCmd.Env, fmt.Sprintf("%s=%s", k, v))
			}

			stdoutPipe, errOut := dryRunCmd.StdoutPipe()
			stderrPipe, errErr := dryRunCmd.StderrPipe()

			if errOut == nil && errErr == nil {
				if err := dryRunCmd.Start(); err == nil {
					var logMu sync.Mutex
					appendLog := func(source LogSource, txt string) {
						logMu.Lock()
						defer logMu.Unlock()
						result.Logs = append(result.Logs, LogEntry{
							Timestamp: time.Now(),
							Source:    source,
							Text:      txt,
						})
					}

					var wg sync.WaitGroup
					wg.Add(2)
					go func() {
						defer wg.Done()
						pipeToFunc(stdoutPipe, func(t string) { appendLog(SourceStdout, t) })
					}()
					go func() {
						defer wg.Done()
						pipeToFunc(stderrPipe, func(t string) { appendLog(SourceStderr, t) })
					}()

					wg.Wait()
					waitErr := dryRunCmd.Wait()
					if waitErr != nil {
						if exitErr, ok := waitErr.(*exec.ExitError); ok {
							result.ExitCode = exitErr.ExitCode()
						} else {
							result.ExitCode = 1
						}
					} else {
						result.ExitCode = 0
					}
					addValidation(fmt.Sprintf("Process dry-run execution completed (Exit Code: %d)", result.ExitCode), result.ExitCode == 0)
				}
			}
		}
	} else if cfg.Builtin == "" {
		addValidation("No command or builtin specified in configuration", false)
	}

	result.Duration = time.Since(startTime)

	// Save last dry run in state
	state.mu.Lock()
	state.LastDryRun = result
	state.mu.Unlock()

	// Log dry-run completion
	state.AddLog(SourceDryRun, fmt.Sprintf("=== Dry Run Completed in %v (Success: %t, Exit Code: %d) ===", result.Duration.Round(time.Millisecond), result.Success, result.ExitCode))
	for _, v := range result.ValidationMsgs {
		state.AddLog(SourceDryRun, v)
	}

	return result, nil
}

// TriggerScheduled forces an immediate single run of a scheduled task (manual trigger)
func (s *Supervisor) TriggerScheduled(id string) error {
	return s.TriggerScheduledWithSource(id, TriggerManual)
}

// TriggerScheduledWithSource runs a scheduled task with a specified trigger source
func (s *Supervisor) TriggerScheduledWithSource(id string, triggerType RunTrigger) error {
	s.mu.RLock()
	state, ok := s.sidecars[id]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("sidecar %s not found", id)
	}

	startTime := time.Now()
	state.AddLog(SourceSupervisor, fmt.Sprintf("Triggering %s execution of scheduled task...", triggerType))

	go func() {
		state.mu.Lock()
		prevStatus := state.Status
		state.Status = StatusExecuting
		state.LastScheduleRun = startTime
		state.mu.Unlock()

		cfg := state.Config
		if cfg.Command == "" {
			state.mu.Lock()
			if cfg.Builtin == "schedule" {
				state.Status = StatusScheduled
			} else {
				state.Status = prevStatus
			}
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, "Scheduled task finished (no child command defined).")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
		cmd.Dir = cfg.EffectiveWorkingDir()
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}

		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()

		if err := cmd.Start(); err != nil {
			state.mu.Lock()
			state.Status = StatusFailed
			state.LastError = fmt.Sprintf("Failed to launch task: %v", err)
			lastErr := state.LastError
			state.mu.Unlock()
			state.AddLog(SourceStderr, lastErr)
			state.AddRunRecord(RunRecord{
				Timestamp: startTime,
				Trigger:   triggerType,
				Duration:  time.Since(startTime),
				ExitCode:  1,
				Error:     lastErr,
				Snippet:   lastErr,
			})
			return
		}

		state.mu.Lock()
		state.PID = cmd.Process.Pid
		state.mu.Unlock()

		var firstSnippet string
		var snippetMu sync.Mutex

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				txt := scanner.Text()
				state.AddLog(SourceStdout, txt)
				snippetMu.Lock()
				if firstSnippet == "" && strings.TrimSpace(txt) != "" {
					firstSnippet = txt
				}
				snippetMu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				txt := scanner.Text()
				state.AddLog(SourceStderr, txt)
				snippetMu.Lock()
				if firstSnippet == "" && strings.TrimSpace(txt) != "" {
					firstSnippet = txt
				}
				snippetMu.Unlock()
			}
		}()

		wg.Wait()
		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		exitCode := 0
		lastError := ""
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
			lastError = fmt.Sprintf("Exit Code %d", exitCode)
		}

		snippetMu.Lock()
		snippet := firstSnippet
		snippetMu.Unlock()
		if snippet == "" {
			if exitCode == 0 {
				snippet = "Completed successfully"
			} else {
				snippet = fmt.Sprintf("Failed with exit code %d", exitCode)
			}
		}

		// Record in RunHistory
		state.AddRunRecord(RunRecord{
			Timestamp: startTime,
			Trigger:   triggerType,
			Duration:  duration,
			ExitCode:  exitCode,
			Error:     lastError,
			Snippet:   snippet,
		})

		state.mu.Lock()
		state.PID = 0
		if exitCode != 0 {
			state.Status = StatusFailed
			state.LastError = fmt.Sprintf("Task exited with error: %v", waitErr)
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, fmt.Sprintf("Task exited with code %d in %v.", exitCode, duration.Round(time.Millisecond)))
		} else {
			if cfg.Builtin == "schedule" {
				state.Status = StatusScheduled
			} else {
				state.Status = prevStatus
			}
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, fmt.Sprintf("Task completed successfully in %v.", duration.Round(time.Millisecond)))
		}
	}()

	return nil
}

// runProcessLoop executes a standard command as a detached background daemon
func (s *Supervisor) runProcessLoop(state *SidecarState) {
	cfg := state.Config
	backoff := 500 * time.Millisecond
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-state.stopChan:
			return
		default:
		}

		logPath := s.registry.GetLogPath(cfg.ID)
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			state.mu.Lock()
			state.Status = StatusFailed
			state.LastError = fmt.Sprintf("Failed to open log file '%s': %v", logPath, err)
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, state.LastError)
			return
		}

		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Dir = cfg.EffectiveWorkingDir()
		// Start in its own detached session (POSIX Setsid)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		// Inject environment
		cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}

		if err := cmd.Start(); err != nil {
			_ = logFile.Close()
			state.mu.Lock()
			state.Status = StatusFailed
			state.LastError = fmt.Sprintf("Failed to start '%s': %v", cfg.Command, err)
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, state.LastError)
			return
		}

		pid := cmd.Process.Pid
		startedAt := time.Now()

		state.mu.Lock()
		state.cmd = cmd
		state.PID = pid
		state.StartedAt = startedAt
		state.Status = StatusRunning
		state.mu.Unlock()

		_ = s.registry.UpdateState(PersistedState{
			ID:         cfg.ID,
			PID:        pid,
			Status:     string(StatusRunning),
			StartedAt:  startedAt,
			LogFile:    logPath,
			Command:    cfg.Command,
			Args:       cfg.Args,
			WorkingDir: cfg.WorkingDir,
			Schedule:   cfg.Schedule,
		}, false)

		state.AddLog(SourceSupervisor, fmt.Sprintf("Process started detached with PID %d (logging to %s)", pid, filepath.Base(logPath)))

		// Start background log tailer for the UI
		go TailFile(logPath, state, state.stopChan)

		// Wait for process to exit
		waitErr := cmd.Wait()
		_ = logFile.Close()

		exitCode := 0
		if waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		state.mu.Lock()
		state.LastExitCode = exitCode
		state.StoppedAt = time.Now()
		state.PID = 0
		state.CPUPercent = 0
		state.MemoryBytes = 0
		state.cmd = nil
		state.mu.Unlock()

		_ = s.registry.UpdateState(PersistedState{ID: cfg.ID}, true)

		// Check if stop requested
		select {
		case <-state.stopChan:
			state.mu.Lock()
			state.Status = StatusStopped
			state.mu.Unlock()
			return
		default:
		}

		// Apply Restart Policy
		shouldRestart := false
		switch cfg.RestartPolicy {
		case config.RestartAlways:
			shouldRestart = true
		case config.RestartOnFailure:
			shouldRestart = exitCode != 0
		case config.RestartNever:
			shouldRestart = false
		default:
			shouldRestart = true
		}

		if !shouldRestart {
			state.mu.Lock()
			if exitCode != 0 {
				state.Status = StatusFailed
				state.LastError = fmt.Sprintf("Process exited with code %d", exitCode)
			} else {
				state.Status = StatusStopped
			}
			state.mu.Unlock()
			state.AddLog(SourceSupervisor, fmt.Sprintf("Process exited with code %d. Restart policy: %s (will not restart).", exitCode, cfg.RestartPolicy))
			return
		}

		state.mu.Lock()
		state.Restarts++
		state.Status = StatusBackoff
		state.mu.Unlock()

		state.AddLog(SourceSupervisor, fmt.Sprintf("Process exited with code %d. Restarting in %v (Restart #%d)...", exitCode, backoff, state.Restarts))

		select {
		case <-state.stopChan:
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// runBuiltinScheduleLoop runs cron / timer for builtin schedule sidecars
func (s *Supervisor) runBuiltinScheduleLoop(state *SidecarState) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	state.AddLog(SourceSupervisor, "Builtin scheduler initialized and active.")

	for {
		select {
		case <-state.stopChan:
			return
		case t := <-ticker.C:
			state.mu.Lock()
			state.NextScheduleRun = t.Add(30 * time.Second)
			state.mu.Unlock()
			_ = s.TriggerScheduledWithSource(state.Config.ID, TriggerCron)
		}
	}
}

// metricsLoop periodically updates CPU and Memory for running processes
// metricSampleEveryNTicks controls how often (in metricsLoop ticks) a
// CPU/memory sample is appended to each sidecar's sparkline history. Live
// gauges still refresh every tick; history is thinned to one sample per 5s.
const metricSampleEveryNTicks = 5

func (s *Supervisor) metricsLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	tick := 0

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			tick++
			recordSample := tick%metricSampleEveryNTicks == 0

			s.mu.RLock()
			for _, state := range s.sidecars {
				state.mu.RLock()
				pid := state.PID
				status := state.Status
				state.mu.RUnlock()

				if status == StatusRunning && pid > 0 {
					cpu, mem := readLinuxMetrics(pid)
					state.mu.Lock()
					state.CPUPercent = cpu
					state.MemoryBytes = mem
					state.mu.Unlock()

					if recordSample {
						state.AddMetricSample(cpu, mem)
					}
				}
			}
			s.mu.RUnlock()
		}
	}
}

// Shutdown detaches the supervisor from sidecars and stops background tickers
// Running detached background processes are NOT terminated so they persist in the OS
func (s *Supervisor) Shutdown() {
	close(s.stopChan)
}

// ShutdownAndStopAll terminates all running sidecars and shuts down the supervisor
func (s *Supervisor) ShutdownAndStopAll() {
	close(s.stopChan)
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, state := range s.sidecars {
		_ = s.Stop(state.Config.ID)
	}
}

func pipeToFunc(r io.Reader, fn func(string)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fn(scanner.Text())
	}
}

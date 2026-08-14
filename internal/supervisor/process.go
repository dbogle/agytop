package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"agytop/internal/config"
)

// ProcessStatus represents the current lifecycle status of a sidecar
type ProcessStatus string

const (
	StatusStopped   ProcessStatus = "STOPPED"
	StatusRunning   ProcessStatus = "RUNNING"
	StatusScheduled ProcessStatus = "SCHEDULED"
	StatusExecuting ProcessStatus = "EXECUTING"
	StatusFailed    ProcessStatus = "FAILED"
	StatusBackoff   ProcessStatus = "BACKOFF"
)

// LogSource indicates the origin of a log line
type LogSource string

const (
	SourceStdout     LogSource = "stdout"
	SourceStderr     LogSource = "stderr"
	SourceSupervisor LogSource = "supervisor"
	SourceDryRun     LogSource = "dry-run"
)

// LogEntry is a single timestamped log event
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Source    LogSource `json:"source"`
	Text      string    `json:"text"`
}

// DryRunResult captures the diagnostics of a dry-run invocation
type DryRunResult struct {
	SidecarID      string            `json:"sidecar_id"`
	Timestamp      time.Time         `json:"timestamp"`
	Success        bool              `json:"success"`
	ExitCode       int               `json:"exit_code"`
	Duration       time.Duration     `json:"duration"`
	ExecutablePath string            `json:"executable_path"`
	Command        string            `json:"command"`
	Args           []string          `json:"args"`
	WorkingDir     string            `json:"working_dir"`
	Env            map[string]string `json:"env"`
	Logs           []LogEntry        `json:"logs"`
	ValidationMsgs []string          `json:"validation_msgs"`
	NextSchedules  []string          `json:"next_schedules,omitempty"`
}

// RunTrigger indicates whether an execution was scheduled or manual
type RunTrigger string

const (
	TriggerCron   RunTrigger = "CRON"
	TriggerManual RunTrigger = "MANUAL"
)

// RunRecord represents a single past execution of a scheduled sidecar
type RunRecord struct {
	Timestamp time.Time     `json:"timestamp"`
	Trigger   RunTrigger    `json:"trigger"`
	Duration  time.Duration `json:"duration"`
	ExitCode  int           `json:"exit_code"`
	Error     string        `json:"error,omitempty"`
	Snippet   string        `json:"snippet,omitempty"`
}

// SidecarState holds the live runtime state of a managed sidecar
type SidecarState struct {
	mu sync.RWMutex

	Config           config.SidecarConfig `json:"config"`
	Status           ProcessStatus        `json:"status"`
	PID              int                  `json:"pid"`
	StartedAt        time.Time            `json:"started_at"`
	StoppedAt        time.Time            `json:"stopped_at"`
	Restarts         int                  `json:"restarts"`
	LastExitCode     int                  `json:"last_exit_code"`
	LastError        string               `json:"last_error"`
	CPUPercent       float64              `json:"cpu_percent"`
	MemoryBytes      uint64               `json:"memory_bytes"`
	CPUHistory       []float64            `json:"cpu_history,omitempty"`
	MemHistory       []uint64             `json:"mem_history,omitempty"`
	MaxMetricSamples int                  `json:"-"`
	Logs             []LogEntry           `json:"logs"`
	MaxLogs          int                  `json:"-"`
	RunHistory       []RunRecord          `json:"run_history,omitempty"`
	MaxHistory       int                  `json:"-"`
	LastDryRun       *DryRunResult        `json:"last_dry_run,omitempty"`
	LastScheduleRun  time.Time            `json:"last_schedule_run,omitempty"`
	NextScheduleRun  time.Time            `json:"next_schedule_run,omitempty"`

	cmd        *exec.Cmd          `json:"-"`
	cancelFunc context.CancelFunc `json:"-"`
	stopChan   chan struct{}      `json:"-"`
}

// MaxMetricSamplesDefault caps the CPU/memory history ring buffers at 30
// minutes of trendline data, sampled once every 5 seconds by metricsLoop.
const MaxMetricSamplesDefault = 360

// NewSidecarState creates a new state wrapper for a discovered sidecar
func NewSidecarState(cfg config.SidecarConfig) *SidecarState {
	s := &SidecarState{
		Config:           cfg,
		Status:           StatusStopped,
		MaxLogs:          1000,
		Logs:             make([]LogEntry, 0, 100),
		MaxHistory:       50,
		RunHistory:       make([]RunRecord, 0, 20),
		MaxMetricSamples: MaxMetricSamplesDefault,
		CPUHistory:       make([]float64, 0, MaxMetricSamplesDefault),
		MemHistory:       make([]uint64, 0, MaxMetricSamplesDefault),
		stopChan:         make(chan struct{}),
	}
	if cfg.Schedule != "" {
		s.NextScheduleRun = time.Now().Add(1 * time.Minute)
	}
	return s
}

// AddLog appends a log entry in a thread-safe ring-buffer manner
func (s *SidecarState) AddLog(source LogSource, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Source:    source,
		Text:      text,
	}

	if len(s.Logs) >= s.MaxLogs {
		s.Logs = append(s.Logs[1:], entry)
	} else {
		s.Logs = append(s.Logs, entry)
	}
}

// GetLogs returns a copy of the log buffer
func (s *SidecarState) GetLogs() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]LogEntry, len(s.Logs))
	copy(res, s.Logs)
	return res
}

// ClearLogs resets the log buffer
func (s *SidecarState) ClearLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Logs = make([]LogEntry, 0, 100)
}

// AddRunRecord records a past execution run in a bounded ring-buffer
func (s *SidecarState) AddRunRecord(record RunRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.RunHistory) >= s.MaxHistory {
		s.RunHistory = append(s.RunHistory[1:], record)
	} else {
		s.RunHistory = append(s.RunHistory, record)
	}
}

// GetRunHistory returns a copy of the recorded execution history
func (s *SidecarState) GetRunHistory() []RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]RunRecord, len(s.RunHistory))
	copy(res, s.RunHistory)
	return res
}

// AddMetricSample appends a CPU/memory sample to the rolling history ring
// buffers used to render Inspector sparklines.
func (s *SidecarState) AddMetricSample(cpuPercent float64, memBytes uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.CPUHistory) >= s.MaxMetricSamples {
		s.CPUHistory = append(s.CPUHistory[1:], cpuPercent)
	} else {
		s.CPUHistory = append(s.CPUHistory, cpuPercent)
	}

	if len(s.MemHistory) >= s.MaxMetricSamples {
		s.MemHistory = append(s.MemHistory[1:], memBytes)
	} else {
		s.MemHistory = append(s.MemHistory, memBytes)
	}
}

// GetRunStats computes overall run count, success rate %, successes, and failures
func (s *SidecarState) GetRunStats() (total int, successRate float64, successes int, failures int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total = len(s.RunHistory)
	if total == 0 {
		return 0, 100.0, 0, 0
	}

	for _, r := range s.RunHistory {
		if r.ExitCode == 0 {
			successes++
		} else {
			failures++
		}
	}

	successRate = (float64(successes) / float64(total)) * 100.0
	return total, successRate, successes, failures
}

// Snapshot returns a point-in-time copy of the state
func (s *SidecarState) Snapshot() SidecarState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logsCopy := make([]LogEntry, len(s.Logs))
	copy(logsCopy, s.Logs)

	historyCopy := make([]RunRecord, len(s.RunHistory))
	copy(historyCopy, s.RunHistory)

	cpuHistCopy := make([]float64, len(s.CPUHistory))
	copy(cpuHistCopy, s.CPUHistory)

	memHistCopy := make([]uint64, len(s.MemHistory))
	copy(memHistCopy, s.MemHistory)

	var dryRunCopy *DryRunResult
	if s.LastDryRun != nil {
		dr := *s.LastDryRun
		dryRunCopy = &dr
	}

	return SidecarState{
		Config:          s.Config,
		Status:          s.Status,
		PID:             s.PID,
		StartedAt:       s.StartedAt,
		StoppedAt:       s.StoppedAt,
		Restarts:        s.Restarts,
		LastExitCode:    s.LastExitCode,
		LastError:       s.LastError,
		CPUPercent:      s.CPUPercent,
		MemoryBytes:     s.MemoryBytes,
		CPUHistory:      cpuHistCopy,
		MemHistory:      memHistCopy,
		Logs:            logsCopy,
		RunHistory:      historyCopy,
		LastDryRun:      dryRunCopy,
		LastScheduleRun: s.LastScheduleRun,
		NextScheduleRun: s.NextScheduleRun,
	}
}

// Helper: Read process memory and CPU on Linux (/proc/<pid>/stat and status)
func readLinuxMetrics(pid int) (cpu float64, mem uint64) {
	if pid <= 0 {
		return 0, 0
	}

	// Read memory from /proc/<pid>/statm (pages: total, resident, shared...)
	statmData, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err == nil {
		fields := strings.Fields(string(statmData))
		if len(fields) >= 2 {
			if pages, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				pageSize := uint64(os.Getpagesize())
				mem = pages * pageSize
			}
		}
	}

	// Fallback/refinement from /proc/<pid>/status
	if mem == 0 {
		if statusData, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
			scanner := bufio.NewScanner(bytes.NewReader(statusData))
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "VmRSS:") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						if kb, err := strconv.ParseUint(parts[1], 10, 64); err == nil {
							mem = kb * 1024
						}
					}
					break
				}
			}
		}
	}

	// Approximation of CPU percent using ps
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=").Output()
	if err == nil {
		val := strings.TrimSpace(string(out))
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			cpu = parsed
		}
	}

	return cpu, mem
}

// KillProcessGroup terminates the process group of the command
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return nil
	}

	// Attempt graceful SIGTERM to process group
	_ = syscall.Kill(-pid, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(800 * time.Millisecond):
		// Force kill
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		return nil
	}
}

// PipeStream reads lines from an io.Reader and sends them to sidecar logs
func pipeStream(r io.Reader, source LogSource, state *SidecarState) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := scanner.Text()
		state.AddLog(source, text)
	}
}

// FormatBytes formats byte sizes into human readable strings
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

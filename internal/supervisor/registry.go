package supervisor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// PersistedState represents the persistent metadata of a detached background sidecar
type PersistedState struct {
	ID         string    `json:"id"`
	PID        int       `json:"pid"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	LogFile    string    `json:"log_file"`
	Command    string    `json:"command"`
	Args       []string  `json:"args"`
	WorkingDir string    `json:"working_dir"`
	Schedule   string    `json:"schedule,omitempty"`
}

// Registry manages reading and writing the persistent state file
type Registry struct {
	mu       sync.Mutex
	filePath string
	logsDir  string
}

// NewRegistry creates a registry pointing to ~/.agytop/state.json
func NewRegistry() *Registry {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	baseDir := filepath.Join(home, ".agytop")
	logsDir := filepath.Join(baseDir, "logs")
	_ = os.MkdirAll(logsDir, 0755)

	return &Registry{
		filePath: filepath.Join(baseDir, "state.json"),
		logsDir:  logsDir,
	}
}

// GetLogPath returns the persistent log file path for a given sidecar ID
func (r *Registry) GetLogPath(id string) string {
	return filepath.Join(r.logsDir, fmt.Sprintf("%s.log", id))
}

// Load reads persisted sidecar states from disk
func (r *Registry) Load() (map[string]PersistedState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked()
}

// loadLocked reads persisted state; callers must hold r.mu.
func (r *Registry) loadLocked() (map[string]PersistedState, error) {
	data, err := os.ReadFile(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]PersistedState), nil
		}
		return nil, err
	}

	var states map[string]PersistedState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("parse %s: %w", r.filePath, err)
	}
	return states, nil
}

// Save records the map of detached sidecars to disk
func (r *Registry) Save(states map[string]PersistedState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked(states)
}

// saveLocked writes state to disk atomically (temp file + rename) so a
// crash mid-write can never leave state.json truncated or invalid.
// Callers must hold r.mu.
func (r *Registry) saveLocked(states map[string]PersistedState) error {
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(r.filePath)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, r.filePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// UpdateState saves or deletes a single sidecar's persisted record.
// The entire load-modify-save cycle is serialized under r.mu so concurrent
// updates from independent sidecar goroutines can't race and silently
// clobber each other's writes.
func (r *Registry) UpdateState(record PersistedState, remove bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	states, err := r.loadLocked()
	if err != nil {
		return err
	}

	if remove {
		delete(states, record.ID)
	} else {
		states[record.ID] = record
	}

	return r.saveLocked(states)
}

// IsPIDAlive checks if a process with the given PID is currently active in the OS
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	// On Linux, check /proc/<pid> existence
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		return true
	}

	// Fallback using syscall signal 0
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// TerminatePID sends SIGTERM to the process and its process group, falling back to SIGKILL
func TerminatePID(pid int) error {
	if pid <= 0 || !IsPIDAlive(pid) {
		return nil
	}

	// First try process group kill (-pid)
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	// Also send direct to PID
	_ = syscall.Kill(pid, syscall.SIGTERM)

	// Wait up to 1.5s for process to exit
	for i := 0; i < 15; i++ {
		time.Sleep(100 * time.Millisecond)
		if !IsPIDAlive(pid) {
			return nil
		}
	}

	// Force kill if still alive
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return nil
}

// TailFile reads recent lines and continuously tails a log file into SidecarState
func TailFile(logPath string, state *SidecarState, stopChan <-chan struct{}) {
	file, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer file.Close()

	// Read existing lines (up to last 100 lines)
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > 100 {
			lines = lines[1:]
		}
	}
	for _, l := range lines {
		state.AddLog(SourceStdout, l)
	}

	// Tail new lines
	reader := bufio.NewReader(file)
	for {
		select {
		case <-stopChan:
			return
		default:
			line, err := reader.ReadString('\n')
			if err == nil {
				txt := line
				if len(txt) > 0 && txt[len(txt)-1] == '\n' {
					txt = txt[:len(txt)-1]
				}
				if len(txt) > 0 && txt[len(txt)-1] == '\r' {
					txt = txt[:len(txt)-1]
				}
				if txt != "" {
					state.AddLog(SourceStdout, txt)
				}
			} else {
				if err == io.EOF {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return
			}
		}
	}
}

package supervisor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	return NewRegistryAt(filepath.Join(home, ".agytop"))
}

// NewRegistryAt creates a registry rooted at the given base directory.
// Exposed primarily so tests can point it at a temp dir instead of the
// user's real ~/.agytop.
func NewRegistryAt(baseDir string) *Registry {
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

const (
	tailMaxLines  = 100
	tailChunkSize = 64 * 1024
)

// readLastLines returns up to maxLines complete lines from the end of the
// file by seeking backward in fixed-size chunks, along with the file's size
// at read time. Unlike bufio.Scanner it has no per-line length limit and
// never scans more of the file than it needs to.
func readLastLines(f *os.File, maxLines int) (lines []string, size int64, err error) {
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size = info.Size()

	var data []byte
	pos := size
	newlineCount := 0

	for pos > 0 && newlineCount <= maxLines {
		chunkSize := int64(tailChunkSize)
		if chunkSize > pos {
			chunkSize = pos
		}
		pos -= chunkSize

		buf := make([]byte, chunkSize)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, size, err
		}

		newlineCount += bytes.Count(buf, []byte{'\n'})
		data = append(buf, data...)
	}

	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if pos > 0 && len(all) > 0 {
		// The first entry may be a partial line continuing from before our
		// read window; drop it since it can't be a complete line.
		all = all[1:]
	}
	if len(all) > maxLines {
		all = all[len(all)-maxLines:]
	}
	return all, size, nil
}

// TailFile reads recent lines and continuously tails a log file into SidecarState
func TailFile(logPath string, state *SidecarState, stopChan <-chan struct{}) {
	file, err := os.Open(logPath)
	if err != nil {
		return
	}
	defer file.Close()

	lines, size, err := readLastLines(file, tailMaxLines)
	if err == nil {
		for _, l := range lines {
			l = strings.TrimSuffix(l, "\r")
			if l != "" {
				state.AddLog(SourceStdout, l)
			}
		}
	} else {
		size = 0
	}

	if _, err := file.Seek(size, io.SeekStart); err != nil {
		return
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

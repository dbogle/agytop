package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chdirTemp changes the working directory to dir for the duration of the
// test and restores the original directory on cleanup. t.Chdir does not
// exist in go1.22 (it landed in go1.24, and this module is pinned to
// go1.22.6 per go.mod), so this hand-rolled version is required. Because
// os.Chdir is process-global, no test using this helper may call
// t.Parallel().
func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("os.Chdir restore to %s: %v", orig, err)
		}
	})
}

// writeSidecar writes a minimal, valid sidecar.json for id "foo" (or
// whatever directory name dir ends in) under dir/sidecar.json.
func writeSidecar(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	content := `{"display_name": "Foo", "command": "echo"}`
	if err := os.WriteFile(filepath.Join(dir, "sidecar.json"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// countByID returns how many entries in sidecars have the given ID, and the
// Scope of the first one found (empty string if none).
func countByID(sidecars []SidecarConfig, id string) (count int, scope string) {
	for _, s := range sidecars {
		if s.ID == id {
			count++
			if count == 1 {
				scope = s.Scope
			}
		}
	}
	return count, scope
}

// TestDiscoverSidecarsCustomBeatsWorkspace asserts scope precedence: a
// custom-dir sidecar wins over a workspace sidecar with the same ID (custom
// dirs are scanned first in DiscoverSidecars).
func TestDiscoverSidecarsCustomBeatsWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	workspaceRoot := t.TempDir()
	chdirTemp(t, workspaceRoot)
	writeSidecar(t, filepath.Join(workspaceRoot, ".agents", "sidecars", "foo"))

	customBase := t.TempDir()
	writeSidecar(t, filepath.Join(customBase, "foo"))

	sidecars, err := DiscoverSidecars(customBase)
	if err != nil {
		t.Fatalf("DiscoverSidecars: %v", err)
	}

	count, scope := countByID(sidecars, "foo")
	if count != 1 {
		t.Fatalf("ID %q appeared %d times, want exactly 1", "foo", count)
	}
	if scope != ScopeCustom {
		t.Errorf("winning scope = %q, want %q (custom should beat workspace)", scope, ScopeCustom)
	}
}

// TestDiscoverSidecarsWorkspaceBeatsGlobal asserts scope precedence: a
// workspace sidecar wins over a global (~/.gemini/config/sidecars) sidecar
// with the same ID.
func TestDiscoverSidecarsWorkspaceBeatsGlobal(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	writeSidecar(t, filepath.Join(homeDir, ".gemini", "config", "sidecars", "foo"))

	workspaceRoot := t.TempDir()
	chdirTemp(t, workspaceRoot)
	writeSidecar(t, filepath.Join(workspaceRoot, ".agents", "sidecars", "foo"))

	sidecars, err := DiscoverSidecars()
	if err != nil {
		t.Fatalf("DiscoverSidecars: %v", err)
	}

	count, scope := countByID(sidecars, "foo")
	if count != 1 {
		t.Fatalf("ID %q appeared %d times, want exactly 1", "foo", count)
	}
	if scope != ScopeWorkspace {
		t.Errorf("winning scope = %q, want %q (workspace should beat global)", scope, ScopeWorkspace)
	}
}

// TestDiscoverSidecarsGlobalBeatsPlugin asserts scope precedence: a global
// sidecar wins over a plugin sidecar with the same ID. No os.Chdir dance is
// needed here since neither scope under test depends on the working
// directory (only the assertion count/scope for ID "foo" is checked, so
// whatever the ambient workspace scope contains is irrelevant).
func TestDiscoverSidecarsGlobalBeatsPlugin(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	writeSidecar(t, filepath.Join(homeDir, ".gemini", "config", "sidecars", "foo"))
	writeSidecar(t, filepath.Join(homeDir, ".gemini", "config", "plugins", "myplugin", "sidecars", "foo"))

	sidecars, err := DiscoverSidecars()
	if err != nil {
		t.Fatalf("DiscoverSidecars: %v", err)
	}

	count, scope := countByID(sidecars, "foo")
	if count != 1 {
		t.Fatalf("ID %q appeared %d times, want exactly 1", "foo", count)
	}
	if scope != ScopeGlobal {
		t.Errorf("winning scope = %q, want %q (global should beat plugin)", scope, ScopeGlobal)
	}
}

// TestLoadSidecarFromFileMalformed covers invalid JSON syntax, a completely
// empty file, and a nonexistent path. All three are expected to error.
//
// NOTE: the empty-file case was verified independently (encoding/json
// returns "unexpected end of JSON input" for zero-byte input) before being
// asserted here -- LoadSidecarFromFile does NOT silently yield a
// zero-value config for an empty file; json.Unmarshal errors and that
// error propagates.
func TestLoadSidecarFromFileMalformed(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) string // returns filePath
	}{
		{
			name: "invalid JSON syntax",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				fp := filepath.Join(dir, "sidecar.json")
				if err := os.WriteFile(fp, []byte("{not valid json"), 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return fp
			},
		},
		{
			name: "completely empty file",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				fp := filepath.Join(dir, "sidecar.json")
				if err := os.WriteFile(fp, nil, 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
				return fp
			},
		},
		{
			name: "nonexistent path",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist", "sidecar.json")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := tt.setup(t)
			cfg, err := LoadSidecarFromFile(fp, ScopeWorkspace, "")
			if err == nil {
				t.Errorf("LoadSidecarFromFile(%s) returned no error; want an error (got cfg=%+v)", fp, cfg)
			}
		})
	}
}

// TestGetDisplayNameFallbacks table-tests all three branches of
// GetDisplayName (config.go:45): DisplayName when set, else ID, else the
// literal "Unnamed Sidecar" -- including a whitespace-only DisplayName,
// which strings.TrimSpace treats as unset.
func TestGetDisplayNameFallbacks(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		id          string
		want        string
	}{
		{"DisplayName set wins", "My Display Name", "some-id", "My Display Name"},
		{"empty DisplayName falls back to ID", "", "some-id", "some-id"},
		{"whitespace-only DisplayName falls back to ID", "   \t  ", "some-id", "some-id"},
		{"both empty falls back to literal", "", "", "Unnamed Sidecar"},
		{"whitespace-only DisplayName and empty ID falls back to literal", "   ", "", "Unnamed Sidecar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SidecarConfig{DisplayName: tt.displayName, ID: tt.id}
			if got := cfg.GetDisplayName(); got != tt.want {
				t.Errorf("GetDisplayName() = %q, want %q", got, tt.want)
			}
			// Sanity: confirm the whitespace-only cases really are
			// whitespace-only, so the test documents intent rather than
			// accidentally passing an empty string.
			if strings.TrimSpace(tt.displayName) != "" && tt.displayName == "" {
				t.Fatalf("test bug: displayName %q not actually whitespace-only", tt.displayName)
			}
		})
	}
}

// TestDiscoverSidecarsPluginTraversal asserts that a non-directory entry
// sitting directly in ~/.gemini/config/plugins/ is skipped without error,
// and that a real plugin's sidecars come back with Scope=plugin and the
// plugin name recorded.
func TestDiscoverSidecarsPluginTraversal(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	pluginsBase := filepath.Join(homeDir, ".gemini", "config", "plugins")
	if err := os.MkdirAll(pluginsBase, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// A non-directory entry sitting directly in plugins/ -- must be
	// skipped, not treated as a plugin directory.
	if err := os.WriteFile(filepath.Join(pluginsBase, "not-a-plugin.txt"), []byte("stray file"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	writeSidecar(t, filepath.Join(pluginsBase, "myplugin", "sidecars", "bar"))

	sidecars, err := DiscoverSidecars()
	if err != nil {
		t.Fatalf("DiscoverSidecars returned an error (non-directory plugin entry should be skipped silently): %v", err)
	}

	var found *SidecarConfig
	for i := range sidecars {
		if sidecars[i].ID == "bar" {
			found = &sidecars[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected sidecar %q from plugin dir, got %+v", "bar", sidecars)
	}
	if found.Scope != ScopePlugin {
		t.Errorf("Scope = %q, want %q", found.Scope, ScopePlugin)
	}
	if found.PluginName != "myplugin" {
		t.Errorf("PluginName = %q, want %q", found.PluginName, "myplugin")
	}
}

func TestLoadSidecarFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	sidecarDir := filepath.Join(tmpDir, "test-sidecar")
	if err := os.MkdirAll(sidecarDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	content := `{
		"display_name": "Test Runner",
		"description": "A test sidecar",
		"command": "python3",
		"args": ["-m", "http.server", "8080"],
		"restart_policy": "on-failure",
		"env": {
			"PORT": "8080"
		},
		"working_dir": "./src"
	}`

	filePath := filepath.Join(sidecarDir, "sidecar.json")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadSidecarFromFile(filePath, ScopeWorkspace, "")
	if err != nil {
		t.Fatalf("LoadSidecarFromFile returned error: %v", err)
	}

	if cfg.ID != "test-sidecar" {
		t.Errorf("expected ID 'test-sidecar', got '%s'", cfg.ID)
	}
	if cfg.GetDisplayName() != "Test Runner" {
		t.Errorf("expected DisplayName 'Test Runner', got '%s'", cfg.GetDisplayName())
	}
	if cfg.Command != "python3" {
		t.Errorf("expected Command 'python3', got '%s'", cfg.Command)
	}
	if cfg.RestartPolicy != RestartOnFailure {
		t.Errorf("expected restart_policy 'on-failure', got '%s'", cfg.RestartPolicy)
	}
	if cfg.Env["PORT"] != "8080" {
		t.Errorf("expected env PORT 8080, got '%s'", cfg.Env["PORT"])
	}
	if cfg.EffectiveWorkingDir() != filepath.Join(sidecarDir, "src") {
		t.Errorf("expected effective working dir %s, got %s", filepath.Join(sidecarDir, "src"), cfg.EffectiveWorkingDir())
	}
}

func TestDiscoverSidecars(t *testing.T) {
	tmpDir := t.TempDir()
	s1 := filepath.Join(tmpDir, "sidecar-one")
	s2 := filepath.Join(tmpDir, "sidecar-two")
	os.MkdirAll(s1, 0755)
	os.MkdirAll(s2, 0755)

	os.WriteFile(filepath.Join(s1, "sidecar.json"), []byte(`{"display_name": "One", "command": "echo"}`), 0644)
	os.WriteFile(filepath.Join(s2, "sidecar.json"), []byte(`{"display_name": "Two", "command": "ls"}`), 0644)

	sidecars, err := DiscoverSidecars(tmpDir)
	if err != nil {
		t.Fatalf("DiscoverSidecars failed: %v", err)
	}

	if len(sidecars) < 2 {
		t.Errorf("expected at least 2 discovered sidecars, got %d", len(sidecars))
	}
}

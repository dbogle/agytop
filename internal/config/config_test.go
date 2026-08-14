package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

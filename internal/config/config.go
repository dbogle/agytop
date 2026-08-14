package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Scope constants for sidecar origin
const (
	ScopeWorkspace = "workspace"
	ScopeGlobal    = "global"
	ScopePlugin    = "plugin"
	ScopeCustom    = "custom"
)

// RestartPolicy constants
const (
	RestartAlways    = "always"
	RestartOnFailure = "on-failure"
	RestartNever     = "never"
)

// SidecarConfig defines the declarative schema for sidecar.json
type SidecarConfig struct {
	ID            string            `json:"id,omitempty"`
	Path          string            `json:"-"`
	Directory     string            `json:"-"`
	Scope         string            `json:"scope,omitempty"`
	PluginName    string            `json:"plugin_name,omitempty"`
	DisplayName   string            `json:"display_name"`
	Description   string            `json:"description,omitempty"`
	Command       string            `json:"command,omitempty"`
	Builtin       string            `json:"builtin,omitempty"`
	Args          []string          `json:"args,omitempty"`
	RestartPolicy string            `json:"restart_policy,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Schedule      string            `json:"schedule,omitempty"`
}

// NormalizedDisplayName returns DisplayName or fallback to ID
func (s *SidecarConfig) GetDisplayName() string {
	if strings.TrimSpace(s.DisplayName) != "" {
		return s.DisplayName
	}
	if s.ID != "" {
		return s.ID
	}
	return "Unnamed Sidecar"
}

// EffectiveWorkingDir returns the configured WorkingDir or the directory containing sidecar.json
func (s *SidecarConfig) EffectiveWorkingDir() string {
	if strings.TrimSpace(s.WorkingDir) != "" {
		if filepath.IsAbs(s.WorkingDir) {
			return s.WorkingDir
		}
		return filepath.Join(s.Directory, s.WorkingDir)
	}
	return s.Directory
}

// LoadSidecarFromFile reads and parses a sidecar.json file
func LoadSidecarFromFile(filePath, scope, pluginName string) (*SidecarConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	var cfg SidecarConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid json in %s: %w", filePath, err)
	}

	dir := filepath.Dir(filePath)
	id := filepath.Base(dir)
	if id == "." || id == "/" {
		id = filepath.Base(filepath.Dir(filePath))
	}

	cfg.ID = id
	cfg.Path = filePath
	cfg.Directory = dir
	cfg.Scope = scope
	cfg.PluginName = pluginName

	if cfg.RestartPolicy == "" {
		cfg.RestartPolicy = RestartAlways
	}

	return &cfg, nil
}

// DiscoverSidecars searches standard Google Antigravity locations for sidecars
func DiscoverSidecars(customDirs ...string) ([]SidecarConfig, error) {
	var sidecars []SidecarConfig
	seenIDs := make(map[string]bool)

	// 1. Custom CLI directories
	for _, customDir := range customDirs {
		if customDir == "" {
			continue
		}
		found := scanSidecarsInDir(customDir, ScopeCustom, "")
		for _, s := range found {
			if !seenIDs[s.ID] {
				seenIDs[s.ID] = true
				sidecars = append(sidecars, s)
			}
		}
	}

	// 2. Workspace Project scoped (.agents/sidecars/ and _agents/sidecars/)
	cwd, err := os.Getwd()
	if err == nil {
		workspaceDirs := []string{
			filepath.Join(cwd, ".agents", "sidecars"),
			filepath.Join(cwd, "_agents", "sidecars"),
		}
		for _, wDir := range workspaceDirs {
			found := scanSidecarsInDir(wDir, ScopeWorkspace, "")
			for _, s := range found {
				if !seenIDs[s.ID] {
					seenIDs[s.ID] = true
					sidecars = append(sidecars, s)
				}
			}
		}
	}

	// 3. Global configuration (~/.gemini/config/sidecars/)
	homeDir, err := os.UserHomeDir()
	if err == nil {
		globalDir := filepath.Join(homeDir, ".gemini", "config", "sidecars")
		found := scanSidecarsInDir(globalDir, ScopeGlobal, "")
		for _, s := range found {
			if !seenIDs[s.ID] {
				seenIDs[s.ID] = true
				sidecars = append(sidecars, s)
			}
		}

		// 4. Plugin scoped (~/.gemini/config/plugins/*/sidecars/)
		pluginsBase := filepath.Join(homeDir, ".gemini", "config", "plugins")
		if entries, err := os.ReadDir(pluginsBase); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					pluginSidecarDir := filepath.Join(pluginsBase, entry.Name(), "sidecars")
					found := scanSidecarsInDir(pluginSidecarDir, ScopePlugin, entry.Name())
					for _, s := range found {
						if !seenIDs[s.ID] {
							seenIDs[s.ID] = true
							sidecars = append(sidecars, s)
						}
					}
				}
			}
		}
	}

	return sidecars, nil
}

// scanSidecarsInDir looks for subdirectories containing sidecar.json or a direct sidecar.json
func scanSidecarsInDir(baseDir, scope, pluginName string) []SidecarConfig {
	var list []SidecarConfig
	stat, err := os.Stat(baseDir)
	if err != nil || !stat.IsDir() {
		return list
	}

	// Check if baseDir itself contains sidecar.json
	directFile := filepath.Join(baseDir, "sidecar.json")
	if _, err := os.Stat(directFile); err == nil {
		if cfg, err := LoadSidecarFromFile(directFile, scope, pluginName); err == nil {
			list = append(list, *cfg)
			return list
		}
	}

	// Read subdirectories
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return list
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subFile := filepath.Join(baseDir, entry.Name(), "sidecar.json")
			if _, err := os.Stat(subFile); err == nil {
				if cfg, err := LoadSidecarFromFile(subFile, scope, pluginName); err == nil {
					list = append(list, *cfg)
				}
			}
		}
	}

	return list
}

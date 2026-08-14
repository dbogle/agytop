package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"agytop/internal/config"
	"agytop/internal/supervisor"
	"agytop/internal/ui"
)

const AppVersion = "v0.1.0"

func main() {
	var (
		customPath  string
		runDemo     bool
		showVersion bool
		dryRunTarget string
		listOnly    bool
	)

	flag.StringVar(&customPath, "config", "", "Custom path to sidecar.json or directory containing sidecars")
	flag.StringVar(&customPath, "c", "", "Custom path (shorthand)")
	flag.BoolVar(&runDemo, "demo", false, "Load built-in demo sidecar configurations")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showVersion, "v", false, "Show version (shorthand)")
	flag.StringVar(&dryRunTarget, "dry-run", "", "Execute non-interactive dry-run diagnostics on a specific sidecar ID")
	flag.StringVar(&dryRunTarget, "d", "", "Dry run target (shorthand)")
	flag.BoolVar(&listOnly, "list", false, "Print discovered sidecars to stdout and exit")
	flag.BoolVar(&listOnly, "l", false, "List (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "agytop %s - Google Antigravity 2.0 Sidecar Supervisor & TUI\n", AppVersion)
		fmt.Fprintf(os.Stderr, "Usage: agytop [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("agytop %s (Google Antigravity 2.0)\n", AppVersion)
		os.Exit(0)
	}

	// Discovery
	var customPaths []string
	if customPath != "" {
		customPaths = append(customPaths, customPath)
	}

	// If demo mode is active, set up demo configurations
	if runDemo {
		demoDir := setupDemoConfigs()
		customPaths = append(customPaths, demoDir)
	}

	configs, err := config.DiscoverSidecars(customPaths...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during discovery: %v\n", err)
	}

	// If no sidecars were found and not explicitly in non-interactive mode, create a default workspace sample or demo
	if len(configs) == 0 && !listOnly && dryRunTarget == "" {
		fmt.Println("No sidecars found in ~/.gemini/config/sidecars or workspace. Generating workspace sample...")
		demoDir := setupDemoConfigs()
		configs, _ = config.DiscoverSidecars(demoDir)
	}

	sup := supervisor.NewSupervisor(configs)
	defer sup.Shutdown()

	// CLI Subcommand: list only
	if listOnly {
		fmt.Printf("Discovered %d Antigravity 2.0 Sidecars:\n", len(configs))
		fmt.Println(strings.Repeat("-", 70))
		fmt.Printf("%-20s %-10s %-15s %s\n", "ID", "SCOPE", "RESTART", "COMMAND/BUILTIN")
		fmt.Println(strings.Repeat("-", 70))
		for _, s := range configs {
			cmdStr := s.Command
			if s.Builtin != "" {
				cmdStr = fmt.Sprintf("builtin:%s (%s)", s.Builtin, s.Schedule)
			}
			fmt.Printf("%-20s %-10s %-15s %s\n", s.ID, s.Scope, s.RestartPolicy, cmdStr)
		}
		os.Exit(0)
	}

	// CLI Subcommand: dry-run single sidecar
	if dryRunTarget != "" {
		result, err := sup.DryRun(dryRunTarget)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error executing dry run on '%s': %v\n", dryRunTarget, err)
			os.Exit(1)
		}
		fmt.Printf("=== Dry Run Report for: %s ===\n", dryRunTarget)
		fmt.Printf("Status:   %v (Exit Code: %d)\n", result.Success, result.ExitCode)
		fmt.Printf("Duration: %v\n", result.Duration)
		fmt.Println("Diagnostics:")
		for _, msg := range result.ValidationMsgs {
			fmt.Printf("  %s\n", msg)
		}
		if len(result.Logs) > 0 {
			fmt.Println("Captured Output:")
			for _, l := range result.Logs {
				fmt.Printf("  [%s] %s\n", l.Source, l.Text)
			}
		}
		if !result.Success {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Interactive TUI Mode
	model := ui.NewModel(sup)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

// setupDemoConfigs writes demonstration sidecar configs and scripts
func setupDemoConfigs() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = os.TempDir()
	}
	demoBase := filepath.Join(cwd, ".agents", "sidecars")
	_ = os.MkdirAll(demoBase, 0755)

	// 1. Data Indexer Daemon (Python)
	s1Dir := filepath.Join(demoBase, "data-indexer")
	_ = os.MkdirAll(s1Dir, 0755)
	_ = os.WriteFile(filepath.Join(s1Dir, "worker.py"), []byte(`import os, sys, time
print("[indexer] Initializing search index worker...", flush=True)
if os.environ.get("AGY_DRY_RUN") == "1":
    print("[indexer] DRY RUN VALIDATION SUCCESSFUL. Exiting probe.", flush=True)
    sys.exit(0)
step = 0
while True:
    step += 1
    print(f"[indexer] Synced {step * 142} codebase embeddings. Health: OK.", flush=True)
    time.sleep(2)
`), 0755)

	_ = os.WriteFile(filepath.Join(s1Dir, "sidecar.json"), []byte(`{
  "display_name": "Codebase Embeddings Indexer",
  "description": "Background worker maintaining vector embeddings for Antigravity code search",
  "command": "python3",
  "args": ["worker.py"],
  "restart_policy": "always",
  "env": {
    "INDEX_REFRESH_RATE": "2s",
    "EMBEDDING_MODEL": "gemini-embedding-001"
  }
}`), 0644)

	// 2. Cron Task Scheduler (Builtin schedule)
	s2Dir := filepath.Join(demoBase, "cron-nightly-report")
	_ = os.MkdirAll(s2Dir, 0755)
	_ = os.WriteFile(filepath.Join(s2Dir, "report.sh"), []byte(`#!/bin/bash
echo "[cron] Running scheduled Antigravity health snapshot..."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[cron] Schedule Dry-Run check passed. Ready for next cron trigger."
    exit 0
fi
echo "[cron] Generating snapshot metrics at $(date)... Done."
`), 0755)

	_ = os.WriteFile(filepath.Join(s2Dir, "sidecar.json"), []byte(`{
  "display_name": "Nightly Health Reporter",
  "description": "Scheduled Antigravity health snapshot and report generator",
  "builtin": "schedule",
  "command": "bash",
  "args": ["report.sh"],
  "schedule": "0 0 * * *",
  "restart_policy": "never"
}`), 0644)

	// 3. Telemetry Bridge Daemon
	s3Dir := filepath.Join(demoBase, "telemetry-bridge")
	_ = os.MkdirAll(s3Dir, 0755)
	_ = os.WriteFile(filepath.Join(s3Dir, "bridge.sh"), []byte(`#!/bin/bash
echo "[telemetry] Connected to local IPC socket."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[telemetry] IPC socket probe successful."
    exit 0
fi
count=0
while true; do
    count=$((count+1))
    echo "[telemetry] Heartbeat #$count: latency 1.2ms, packet loss 0.0%"
    sleep 3
done
`), 0755)

	_ = os.WriteFile(filepath.Join(s3Dir, "sidecar.json"), []byte(`{
  "display_name": "IPC Telemetry Bridge",
  "description": "Relays diagnostic events to Antigravity 2.0 desktop auxiliary pane",
  "command": "bash",
  "args": ["bridge.sh"],
  "restart_policy": "always"
}`), 0644)

	// 4. Failing Service (Demonstrating backoff and error visualization)
	s4Dir := filepath.Join(demoBase, "flaky-service")
	_ = os.MkdirAll(s4Dir, 0755)
	_ = os.WriteFile(filepath.Join(s4Dir, "flaky.sh"), []byte(`#!/bin/bash
echo "[flaky] Starting transient background check..."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[flaky] Dry run check succeeded."
    exit 0
fi
sleep 1
echo "[flaky] ERROR: Connection to remote daemon timed out after 1000ms" >&2
exit 42
`), 0755)

	_ = os.WriteFile(filepath.Join(s4Dir, "sidecar.json"), []byte(`{
  "display_name": "Flaky Remote Sync",
  "description": "Demonstrates supervisor crash detection and exponential backoff",
  "command": "bash",
  "args": ["flaky.sh"],
  "restart_policy": "on-failure"
}`), 0644)

	return demoBase
}

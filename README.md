# agytop ⚡

[![CI](https://github.com/dbogle/agytop/actions/workflows/ci.yml/badge.svg)](https://github.com/dbogle/agytop/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-emerald.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/dbogle/agytop)](go.mod)
[![Release](https://img.shields.io/github/v/release/dbogle/agytop?include_prereleases&color=blue)](https://github.com/dbogle/agytop/releases)

> **Interactive Terminal Resource Monitor & Supervisor for Google Antigravity 2.0 Sidecars.**

`agytop` is a high-fidelity Terminal User Interface (TUI) built in **Go** using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Bubbles](https://github.com/charmbracelet/bubbles).

---

## 🌟 Key Features

* **Multi-Scope Discovery Engine**:
  * Automatically detects sidecars in:
    * **Workspace Project**: `.agents/sidecars/` & `_agents/sidecars/`
    * **Global User**: `~/.gemini/config/sidecars/`
    * **Plugins**: `~/.gemini/config/plugins/*/sidecars/`
    * **Custom Paths**: Passed via `-c` / `--config` flag.
* **Continuous Daemons vs. Scheduled Tasks**:
  * Explicitly differentiates between continuous daemons (`[RUNNING]`, `[STOPPED]`) and cron-scheduled tasks (`[SCHEDULED]`, `[EXECUTING]`).
  * Live ASCII telemetry gauges for CPU and RSS Memory utilization.
  * Automatic restart handling (`always`, `on-failure`, `never`) with exponential backoff.
* **⚡ Dry-Run Diagnostics Engine (`d` key)**:
  * Performs isolated, non-destructive validation of sidecar configurations.
  * Injects `AGY_DRY_RUN=1`, `DRY_RUN=true`, and `ANTIGRAVITY_SIDECAR_DRY_RUN=1`.
  * Validates binary paths, directory permissions, syntax, and generates simulated cron timelines.
* **Real-time Log Streaming**:
  * Color-coded tags (`[PROCESS]`, `[STDERR]`, `[SYSTEM]`, `[DRY-RUN]`) with timestamps.
  * Auto-scroll follow mode (`a`), full-screen log maximize (`l`), and log buffer reset (`c`).
* **Search & Filtering (`/`)**:
  * Real-time fuzzy filtering across sidecar IDs, display names, scopes, and health statuses.

---

## 🚀 Installation & Quickstart

### Build from source

```bash
# Clone repository
git clone https://github.com/dbogle/agytop.git
cd agytop

# Build binary
go build -o bin/agytop ./cmd/agytop

# Launch interactive TUI
./bin/agytop

# Launch with built-in demonstration suite
./bin/agytop --demo
```

### CLI Subcommands & Flags

```
Usage: agytop [options]

Options:
  -c, --config string    Custom path to sidecar.json or directory containing sidecars
      --demo             Load built-in demo sidecar configurations
  -d, --dry-run string   Execute non-interactive dry-run diagnostics on a specific sidecar ID
  -l, --list             Print discovered sidecars to stdout and exit
  -v, --version          Show version information
```

---

## ⌨️ Keybindings

| Key | Action |
| :--- | :--- |
| `↑` / `k`, `↓` / `j` | Navigate sidecar list |
| `Tab` / `Shift+Tab` | Cycle focus between List, Inspector, and Logs |
| **`s`** | **Start** daemon or **Arm** scheduled cron timer |
| **`x`** | **Stop / Terminate** active process |
| **`r`** | **Restart** active process |
| **`d`** | **Trigger Dry-Run Diagnostics Modal** |
| **`t`** | **Trigger Immediate Execution** (for scheduled sidecars) |
| **`v`** | **View Raw `sidecar.json`** definition |
| **`/`** | **Filter & Search** sidecars |
| `l` | Toggle maximized full-screen log stream |
| `a` | Toggle log auto-scroll (Follow mode) |
| `c` | Clear log buffer for selected sidecar |
| `?` / `h` | Open **Help Cheat Sheet** |
| `Esc` | Close modal / clear search filter |
| `q` / `Ctrl+C` | Shutdown supervisor & Quit |

---

## 📋 Sidecar Configuration Specification (`sidecar.json`)

Place `sidecar.json` in your workspace (`.agents/sidecars/<name>/sidecar.json`) or global directory (`~/.gemini/config/sidecars/<name>/sidecar.json`):

### Continuous Daemon:
```json
{
  "display_name": "Codebase Embeddings Indexer",
  "description": "Background worker maintaining vector embeddings for Antigravity code search",
  "command": "python3",
  "args": ["worker.py"],
  "restart_policy": "always",
  "working_dir": "./",
  "env": {
    "INDEX_REFRESH_RATE": "2s",
    "EMBEDDING_MODEL": "gemini-embedding-001"
  }
}
```

### Scheduled Cron Task:
```json
{
  "display_name": "Nightly Health Reporter",
  "description": "Scheduled Antigravity health snapshot generator",
  "builtin": "schedule",
  "command": "bash",
  "args": ["report.sh"],
  "schedule": "0 0 * * *",
  "restart_policy": "never"
}
```

# Google Antigravity 2.0 Sidecar Supervisor & TUI (`agy-sidecars`)

An interactive Terminal User Interface (TUI) and supervisor designed for monitoring and controlling **Google Antigravity 2.0 Sidecars**.

Built in **Go** using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lipgloss](https://github.com/charmbracelet/lipgloss), and [Bubbles](https://github.com/charmbracelet/bubbles).

---

## 🌟 Features

* **Multi-Scope Discovery Engine**:
  * Automatically detects sidecars in:
    * **Workspace Project**: `.agents/sidecars/` & `_agents/sidecars/`
    * **Global User**: `~/.gemini/config/sidecars/`
    * **Plugins**: `~/.gemini/config/plugins/*/sidecars/`
    * **Custom paths**: Passed via `-c` / `--config` flag.
* **Process Lifecycle Supervision**:
  * Start, Stop, and Restart sidecar processes.
  * Real-time metrics: Process ID (PID), CPU usage %, RSS memory footprint, and live uptime.
  * Restart policies: `always`, `on-failure`, and `never` with exponential backoff handling.
  * Builtin `schedule` (cron) support with next-run and last-run indicators.
* **⚡ Dry-Run Probe & Diagnostics (`d` key)**:
  * Performs isolated, non-destructive validation of sidecar configurations.
  * Injects `AGY_DRY_RUN=1`, `DRY_RUN=true`, and `ANTIGRAVITY_SIDECAR_DRY_RUN=1`.
  * Verifies binary existence in `PATH`, directory permissions, syntax check, and captures probe output into a dedicated diagnostics modal.
* **Live Streaming Output**:
  * Auto-scrolling, color-coded log viewer (`[stdout]`, `[stderr]`, `[system]`, `[dryrun]`).
  * Follow mode toggle (`a`), fullscreen log maximize (`l`), and clear buffer (`c`).
* **Search & Filtering**:
  * Interactive instant fuzzy/prefix search (`/`) across names, scopes, commands, and health statuses.

---

## 🚀 Quickstart

### 1. Build and Run

```bash
# Build the binary
go build -o bin/agy-sidecars ./cmd/agy-sidecars

# Launch the interactive TUI
./bin/agy-sidecars

# Or launch with sample demonstration sidecars
./bin/agy-sidecars --demo
```

### 2. CLI Options

```
Usage: agy-sidecars [options]

Options:
  -c, --config string    Custom path to sidecar.json or directory containing sidecars
      --demo             Load built-in demo sidecar configurations
  -d, --dry-run string   Execute non-interactive dry-run diagnostics on a specific sidecar ID
  -l, --list             Print discovered sidecars to stdout and exit
  -v, --version          Show version information
```

---

## ⌨️ Keybindings

| Keybinding | Action |
| :--- | :--- |
| `↑` / `k`, `↓` / `j` | Navigate sidecars in list |
| `Tab` / `Shift+Tab` | Cycle focus across Panes (Sidecars List, Inspector, Logs) |
| **`s`** | **Start** selected sidecar |
| **`x`** | **Stop / Terminate** selected sidecar |
| **`r`** | **Restart** selected sidecar |
| **`d`** | **Trigger Dry-Run Diagnostics** (opens modal) |
| **`t`** | **Trigger Immediate Run** (for scheduled / cron sidecars) |
| **`v`** | **View Raw `sidecar.json`** configuration |
| **`/`** | **Filter & Search** sidecars |
| `l` | Toggle maximized full-screen log stream |
| `a` | Toggle log auto-scroll (Follow mode) |
| `c` | Clear log buffer for selected sidecar |
| `?` / `h` | Open **Help Cheat Sheet** |
| `Esc` | Close modal / clear search filter |
| `q` / `Ctrl+C` | Quit |

---

## 📋 Sidecar Configuration Specification (`sidecar.json`)

Place `sidecar.json` in your workspace (`.agents/sidecars/<name>/sidecar.json`) or global folder (`~/.gemini/config/sidecars/<name>/sidecar.json`):

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

For scheduled cron tasks:

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

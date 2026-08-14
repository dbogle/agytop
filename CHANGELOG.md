# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Removed
- **Windows release builds.** The `windows` target was listed in the GoReleaser
  configuration but had never compiled: process supervision depends on POSIX
  detached process groups (`Setsid`), `syscall.Kill`, and `/proc`. Releases are
  now Linux and macOS only. Windows support is tracked as a future port rather
  than shipped as a broken artifact.

### Fixed
- **`--version` reported `v0.1.0` in every released binary.** `AppVersion` was
  declared as a `const`, and the linker's `-X` flag can only patch string
  variables, so GoReleaser's version injection was silently a no-op.
- **`go vet ./...` failed with 8 copylocks errors.** Snapshots of sidecar state
  carried a copy of the live `sync.RWMutex` across the supervisor/UI boundary.
  Snapshots are now a distinct mutex-free `supervisor.StateView` type, making
  the copy semantics explicit and the value-passing safe by construction.
- **The `c` (clear logs) keybinding never actually cleared anything.** It
  operated on `m.selectedState()`, a point-in-time `StateView` copy, so the
  live log buffer was untouched and the next 200ms poll tick overwrote the
  copy with the still-full original, making the logs silently reappear.
  `Supervisor.ClearLogs(id)` now clears the live `SidecarState`'s buffer
  directly (in-memory only -- the on-disk log file and its tailer offset are
  left alone), and the UI refreshes immediately instead of waiting on the
  next tick.

### Added
- CI now enforces `go vet ./...` on every matrix leg and `gofmt -s -l .` on one,
  closing the gap that let the above two defects reach `main`.
- First tests for `internal/ui`, covering view rendering, both modal renderers,
  keybinding routing, and the `GetRunStats` zero-run branch.

---

## [0.1.0] - 2026-08-14

### Added
- **Multi-Scope Discovery Engine**: Automatic detection of sidecars across workspace projects (`.agents/sidecars/`, `_agents/sidecars/`), global user configs (`~/.gemini/config/sidecars/`), plugin directories, and custom CLI paths.
- **Interactive 3-Pane TUI**: Built with Go, Bubble Tea, Lipgloss, and Bubbles adhering to the Stitch *Technical Brutalism* design specification.
- **Continuous vs. Scheduled Lifecycle Management**:
  - Continuous daemons: `RUNNING`, `STOPPED`, `FAILED`, and `BACKOFF` with exponential restart backoff.
  - Scheduled cron tasks: `SCHEDULED` (armed/waiting for timer) and `EXECUTING` (active run).
- **⚡ Dry-Run Diagnostics Engine (`d` key / `--dry-run`)**:
  - Non-destructive execution probe injecting `AGY_DRY_RUN=1` and `DRY_RUN=true`.
  - Executable path resolution, directory permission check, and syntax validation.
  - Simulated cron trigger timeline graph generator.
- **Telemetry Gauges**: Real-time ASCII bar gauges for live CPU % and RSS Memory utilization.
- **Live Output Stream**: Auto-scrolling, color-coded log viewer with source badges (`[PROCESS]`, `[STDERR]`, `[SYSTEM]`, `[DRY-RUN]`), auto-scroll toggle (`a`), and full-screen maximize (`l`).
- **Search & Filtering**: Real-time interactive filter (`/`) by ID, display name, scope, or status.
- **CLI Subcommands**:
  - `agytop --list`: Table overview of all discovered sidecars.
  - `agytop --dry-run <id>`: Non-interactive CLI diagnostics report.
  - `agytop --demo`: Instant launch with built-in companion demonstration sidecars.
- **Open Source Automation**:
  - Multi-OS GitHub Actions CI workflow (Ubuntu & macOS) with Go concurrency race detection (`-race`).
  - GoReleaser configuration for automated cross-platform release builds (Linux, macOS, Windows).
  - MIT License and contributing guidelines.

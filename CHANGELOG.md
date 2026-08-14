# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [2.0.0] - 2026-08-14

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

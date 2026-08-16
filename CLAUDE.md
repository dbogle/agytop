# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`agytop` is a terminal UI (TUI) written in Go that discovers, supervises, and monitors "sidecar" processes for Google Antigravity 2.0 — background daemons or cron-style scheduled tasks declared via `sidecar.json` files. It's built on Bubble Tea (Elm-architecture TUI framework), Lipgloss (styling), and Bubbles (textinput/viewport components).

## Common commands

```bash
# Build
go build -o bin/agytop ./cmd/agytop

# Run all tests with race detector (matches CI)
go test -v -race ./...

# Run a single test
go test -v -race -run TestSupervisorLifecycleAndDryRun ./internal/supervisor/

# Static analysis
go vet ./...

# Format (required before PRs per CONTRIBUTING.md)
gofmt -s -w .

# Run interactively with bundled demo sidecars (writes .agents/sidecars/ into CWD)
./bin/agytop --demo

# Non-interactive checks
./bin/agytop --list                 # print discovered sidecars and exit
./bin/agytop --dry-run <sidecar-id> # validate one sidecar without launching it for real
```

CI (`.github/workflows/ci.yml`) runs `go test -v -race ./...` and a build+smoke-test (`--version`, `--demo --list`) across Go 1.22.x/1.23.x on Linux and macOS. Releases are cut via GoReleaser (`.goreleaser.yaml`) on `v*` tags, cross-compiling linux/darwin/windows and injecting the version into `main.AppVersion` via ldflags.

## Architecture

Three packages under `internal/`, wired together by `cmd/agytop/main.go`:

```
config.DiscoverSidecars() → []SidecarConfig → supervisor.NewSupervisor() → ui.NewModel() → tea.Program
```

### `internal/config` — discovery & schema

`SidecarConfig` is the declarative schema parsed from each `sidecar.json`. `DiscoverSidecars` scans, in order, and **dedupes by ID on first match** (so earlier scopes win over later ones):

1. Custom paths passed via `-c`/`--config`
2. Workspace: `<cwd>/.agents/sidecars/` and `<cwd>/_agents/sidecars/`
3. Global: `~/.gemini/config/sidecars/`
4. Plugins: `~/.gemini/config/plugins/*/sidecars/`

A sidecar's ID is derived from its containing directory name. A config with `"builtin": "schedule"` is a scheduled task (cron-like); anything else with a `command` is a continuous daemon.

### `internal/supervisor` — process lifecycle

- `Supervisor` holds a `map[string]*SidecarState` plus a stable `order` slice (for consistent UI listing) and a `Registry`.
- **Continuous daemons** (`runProcessLoop`) are launched **detached** (`SysProcAttr{Setsid: true}`) with stdout/stderr redirected to `~/.agytop/logs/<id>.log`, so they survive the TUI exiting. Crash/exit triggers restart-policy handling (`always` / `on-failure` / `never`) with exponential backoff (500ms → 30s cap).
- **Scheduled tasks** (`runBuiltinScheduleLoop`) currently tick on a fixed 30-second interval and call `TriggerScheduledWithSource` — note the `schedule` cron expression in the config is *not* actually parsed/respected yet, it's informational/displayed only. Each run is recorded as a `RunRecord` in a bounded ring buffer (`SidecarState.RunHistory`, capped at `MaxHistory`), viewable via the History modal (`h`).
- `Registry` (`internal/supervisor/registry.go`) persists detached-process metadata to `~/.agytop/state.json` and manages `~/.agytop/logs/`. On startup, `Supervisor.reAttachRunningSidecars` reads this file and reconnects to any still-alive PIDs (checked via `/proc/<pid>` on Linux), resuming log tailing (`TailFile`) rather than relaunching.
- `DryRun` performs non-destructive validation (working dir, resolved executable path, a short actual invocation with a 3s timeout) with `AGY_DRY_RUN=1`, `DRY_RUN=true`, `ANTIGRAVITY_SIDECAR_DRY_RUN=1` injected into the environment — used by both `-d`/`--dry-run` CLI mode and the `d` key in the TUI.
- Every `SidecarState` has its own `sync.RWMutex`; `Snapshot()`/`GetAllStates()` return deep-ish copies so the UI never touches live state directly. A background `metricsLoop` polls CPU/RSS from `/proc/<pid>` (via `/proc/<pid>/statm` + `status`) and `ps` every second for running processes.

### `internal/ui` — Bubble Tea TUI

Standard Elm-architecture split across files:
- `model.go`: `Model` struct, `Update` (keybinding dispatch + `tickMsg` polling every 200ms to pull fresh state from the supervisor — the UI is poll-based, not event-pushed), and `View` (three-pane layout: sidecar List / Inspector / Logs, switchable via `Tab`).
- `keymap.go`: `KeyMap` binding table (for reference/help text — actual dispatch is the `switch msg.String()` in `model.go`'s `Update`, keep both in sync when adding a key).
- `modals.go`: full-screen overlays for Dry-Run diagnostics, Help, raw JSON config view, and Run History — `Model.View` short-circuits to these when their `*ModalOpen` flag is set.
- `styles.go`: the "Stitch" Lipgloss theme (deep-zinc palette, status badges/pills, gauges).

When a modal is open, `Update` intercepts key events before the main keybinding switch (see the `if m.dryRunModalOpen { ... }` chain) — new modals need their own guard block ahead of the general keybindings, mirroring the existing ones.

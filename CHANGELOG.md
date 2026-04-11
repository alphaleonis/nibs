# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## v0.2.1 - 2026-04-11

### Fixed
- Web UI now reflects external nib changes (from the CLI, a text editor, or another process) without requiring a server restart. `nibs serve` now starts the filesystem watcher on startup, so the in-memory store stays in sync with the `.nibs/` directory.
- Web UI no longer gets stuck on a "Loading..." indicator after an external nib change. The TreeTable subscription effect was entering an infinite loop (`effect_update_depth_exceeded`) because urql's subscription store emits a fresh wrapper object on every reactive cycle, making reference-based deduplication unreliable. Fixed by deduplicating on event content and wrapping side effects in `untrack()`.

## v0.2.0 - 2026-04-11

### Added
- `nibs config set-prefix <new-prefix>` — new CLI command to change a project's nib ID prefix. Renames every nib file (active + archive), rewrites `parent` and `blocked_by` front-matter references, and atomically updates `.nibs.yml`. Supports `--dry-run` to preview the plan, `--force` to override the git-dirty guard, and `--json` for structured output.
- Install scripts for Linux, macOS, and Windows (`install.sh` / `install.ps1`) invoked via `curl | sh` or `irm | iex` from the README.

### Fixed
- `install.ps1` now works under PowerShell Core (`pwsh`): uses the .NET `System.IO.Compression.ZipFile` API directly to avoid cmdlet shadowing issues that broke `Expand-Archive` in some environments.

### Changed
- Build and dev task runner migrated from mise's `[tasks]` to [go-task](https://taskfile.dev/) (`Taskfile.yml`). mise still manages tool version pinning (Go, golangci-lint, Task itself). Contributors building from source should run `mise install` to pull the pinned tools, then `task build` / `task test` / `task lint`. Runtime and CLI behavior are unchanged.

## v0.1.0 - 2026-04-09

Forked from [beans](https://github.com/hmans/beans). Notable changes from upstream:

### Added
- Svelte 5 web UI with tree table, drag-drop reordering, and inline editing
- `close`, `reorder`, `context` CLI commands
- `--after`, `--before`, `--first` positioning flags on `update`
- Document link relationships
- TUI: Reordering, collapsible trees, wide/narrow mode toggle, two-column mode
- TUI: create flow with type picker and contextual parent inference
- Apache 2.0 license and third-party license collection

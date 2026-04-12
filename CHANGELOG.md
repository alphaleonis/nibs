# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- Detail panel font sizes now match the list view (inherit base 1rem instead of custom 0.8125rem overrides).
- Detail panel title is now larger (1.25rem) for better visual hierarchy.
- Status, Type, Priority, and Estimate selects now show icons (status dot, type icon, priority indicator, estimate abbreviation) in both the trigger and dropdown items, matching the filter bar style.
- Detail panel metadata fields (Status, Type, Priority, Estimate) are laid out horizontally with wrapping instead of a vertical grid, with small labels above each select.
- Editor modal metadata fields use the same stacked-label layout.
- Detail panel max width increased from a fixed 800px to 75% of the container width.

### Fixed
- Dragging the detail panel resize handle below minimum size no longer freezes the UI. The resize handle stays in the DOM (hidden via CSS) so PaneForge can complete its drag cleanup, and the collapse callback is deferred with `requestAnimationFrame`.
- Pressing ESC in the editor modal with unsaved changes no longer closes the modal before the confirm dialog appears. Canceling the dialog now correctly keeps the editor open.
- `task build` no longer runs `go generate ./...` twice. `codegen` was listed as a direct dep of both `build` and `web:build`, and Task does not deduplicate deps that resolve in parallel branches of the DAG.

### Security
- Bump `vite` to `^8.0.8` (from `^8.0.3`) to clear three advisories affecting `vite` 8.0.0–8.0.4: dev-server WebSocket arbitrary file read ([GHSA-p9ff-h696-f583](https://github.com/advisories/GHSA-p9ff-h696-f583)), `server.fs.deny` bypass ([GHSA-v2wj-q39q-566r](https://github.com/advisories/GHSA-v2wj-q39q-566r)), and a path traversal in optimized-deps `.map` handling ([GHSA-4w7w-66w2-5vf9](https://github.com/advisories/GHSA-4w7w-66w2-5vf9)). Only affects contributors running the Vite dev server.

## v0.2.1 - 2026-04-11

### Fixed
- Web UI now updates live when nibs are changed outside the browser (via the CLI, a text editor, or another process). Previously a server restart was required.

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

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## v0.5.0 - 2026-05-06

### Added
- **`--columns` flag on `nibs list` and `nibs links`** — tab-separated tabular output with selectable fields. Comma-separated column names from a closed set: `id, slug, title, status, type, priority, estimate, order, parent, tags, created_at, updated_at`. Output is flat (one row per nib, no per-rel section headers — `links` implies `--flat` semantics: a single deduped row list across all requested rels). Mutually exclusive with `--json`; `nibs list --columns` is also mutually exclusive with `--quiet`. Empty fields render as the empty string; multi-value tag fields are comma-joined; time fields render as RFC3339 (or empty for nil). (Refs: nibs-632y.)
- `nibs plan --with-order` flag to display child order keys; `order` field now always present in `nibs plan --json` output. (Refs: nibs-s5sg.)

### Changed
- **BREAKING:** `nibs links --rel children --order topo` now derives edges from each nib's `blocked_by` front-matter field only. Previously it also treated `#<id>` mentions in bodies as ordering edges, which contradicted the documented "mentions are informational only" contract and created spurious cycles when siblings cross-referenced each other for context. Callers that relied on `#<id>` mentions implicitly producing topo ordering must now declare those dependencies via `blocked_by`. (Refs: nibs-q4pi, decision in nibs-t36b.)

### Fixed
- `nibs --json <cmd>` now produces a single parseable JSON document on stdout for both success and error paths. Cobra's usage block is suppressed on every RunE error (text mode too — usage was meant for `--help` only); in JSON mode the duplicate stderr `Error:` line is also suppressed via a sentinel error. Previously, merged `2>&1 | jq` consumers saw broken JSON, and `nibs <cmd>` errors printed the full flag listing to stderr. (Refs: nibs-382a.)
- `nibs create --after`/`--before`/`--first` now work for root-level nibs. Previously they errored with `"positioning requires a parent"`. (Refs: nibs-d44y.)

## v0.4.1 - 2026-05-04

### Changed
- `nibs prime` now emits a slim ~1 KB prompt with mandatory workflow rules and a directive for the agent to load the full reference on demand. Pass `--full` to emit the complete CLI guide (commands, flags, body section conventions, GraphQL examples). The previous default — a ~17 KB single blob — was being truncated to a 2 KB inline preview by some agent harnesses, causing agents to silently miss command syntax (e.g., `nibs update --after` for reordering). The full reference (`prompt-full.tmpl`) is unchanged from the previous default.

## v0.4.0 - 2026-05-01

### Added
- **Bulk reorder of multiple sibling nibs in one command** — `reorderChildren` GraphQL mutation accepts a declarative full-list (Mode A) and reorders every direct child of `parentId` in the given order (use `parentId: ""` for root-level siblings); requires strict completeness. `reorderSiblings` GraphQL mutation moves a contiguous block of listed siblings as a unit (Mode B), with the parent inferred from the listed nibs and a destination expressed as `--after`, `--before`, or `--first`. CLI: `nibs reorder --children-of <parent>` for Mode A; `nibs reorder <id1> <id2> ... --after <anchor>` (or `--before` / `--first`) for Mode B. Existing single-nib `nibs reorder <id> --after ...` behavior is unchanged.
- **Per-child ifMatch for bulk reorder** — `reorderChildren` and `reorderSiblings` accept `ifMatch: [ChildEtag!]`, threaded from CLI as repeatable `--child-if-match <id>=<etag>`. Pre-validation runs before any writes: a stale or unknown etag aborts the whole operation atomically. Under `require_if_match: true` every listed nib must have an entry; under `false` partial coverage is permitted. `--child-if-match` is mutually exclusive with `--if-match` (which remains the form for single-nib reorder).
- **Flag suggestions for typos** — unknown long flags now surface `Did you mean --foo?` via Cobra's `SetFlagErrorFunc`. Levenshtein-based with maxDist 2 and a skip-on-tie rule (no suggestion when two candidates tie at the minimum distance). Inherits to subcommands through Cobra's parent-walk in `FlagErrorFunc()` — works for local subcommand flags, persistent root flags, and late-registered subcommands.
- **Natural-order position column in `nibs list` tree view** — new leftmost `#` column showing each nib's 1-based position among its siblings (sort-independent, sourced from `Order` keys), so reorder references survive `--sort` changes. Auto-sized width; hidden when not applicable.

### Changed
- `internal/ui/tree.RenderTree` folds the previous `calculateMaxDepth` + `maxVisiblePosition` into a single `treeMetrics` walk; replaces `len(fmt.Sprintf("%d", n))` with `len(strconv.Itoa(n))`. No behavior change.

### Fixed
- `nibs list` short-form now renders `research` type as `R` instead of `?`. `ShortType` and `ShortStatus` are now driven from `config.DefaultTypes` / `config.DefaultStatuses` (first letter uppercased) so future types automatically get a short code; uniqueness invariant is guarded by tests.
- Priority symbols, blocked/blocking indicators, the selection cursor, tree connectors, the divider, and collapse/section cursors fall back to ASCII variants when the terminal cannot display UTF-8 — fixes mojibake (`Γåô` etc.) on Windows consoles using cp1252 or other non-UTF-8 codepages. Platform detection caches once via `sync.Once` (Windows: `GetConsoleOutputCP`); a `term.IsTerminal` precheck keeps redirected stdout on UTF-8 since pipes and files are UTF-8-friendly.
- Test setup helpers in `cmd/` no longer leak `rootCmd` persistent-flag state (`--nibs-path`, `--config`) across tests. New shared `resetRootPersistentFlags` helper is wired into every Cobra-driving setup helper and direct `rootCmd.Execute()` call site, so a leftover `t.TempDir()` path can no longer carry forward into a subsequent test.

## v0.3.1 - 2026-04-27

### Fixed
- `nibs context` now counts `research` nibs as leaf work. Previously `isLeafType` only matched `task`, `bug`, and `feature`, so research nibs were silently dropped from `ActiveTasks`, `NextTasks`, the summary `Progress`, and per-milestone `ContainerSummary.Progress` — an in-progress research nib under an active milestone was invisible to the context view.

## v0.3.0 - 2026-04-24

### Added
- **Body-reference mentions** — `#<id>` tokens in nib bodies are now first-class relationships. Mentions use a `#` sigil followed by a nib ID in short form (`#gx0f`) or full form (`#nibs-gx0f`). Parsing uses the goldmark AST, so mentions inside fenced code blocks, inline code spans, links, images, and raw HTML are skipped; heading text mentions are extracted. Self-references and unresolved tokens drop silently.
- **`nibs links <id>`** — unified relationship-graph query command that replaces the retired `refs` and `deps`. Closed-set relation enum: `mentions-out`, `mentions-in`, `parent`, `children`, `siblings`, `blocking`, `blocked-by`, `ancestors`, `descendants`, `blockers-transitive`, `blocks-transitive`, `mentions-out-transitive`, `mentions-in-transitive`, `neighbours` (expands to the 7 direct rels), and `neighbours-active` (direct rels excluding completed/scrapped). Supports `--depth N|all`, `--order topo` (reusing the toposort engine from deps), `--flat` to collapse multi-rel output, `--json`, and the full list-style filter surface. Filters that don't apply to the requested rel fail with a clear error rather than being silently ignored. Stable envelope shape: `{id, depth?, relations: {<rel>: {nibs: [...]}}}`, the same for single-rel and multi-rel queries.
- **`nibs show --body-chars N` / `--summary`** — truncate body previews when surveying many nibs at once. Rune-aware (multi-byte safe); appends `…` when cut. Applies to default styled / `--json` / `--body-only` output; `--raw` and `--etag-only` stay byte-faithful. JSON emits `body_truncated: true` only when truncation occurred.
- **`nibs show --active` / `--no-mentions`** — filter mention sections in the show view. `--active` drops completed/scrapped entries from both outbound and inbound mentions; `--no-mentions` skips the mention scan entirely.
- **`nibs list --mentions <id>` / `--mentioned-by <id>`** — filter flags for body-reference relationships, mirroring `--blocked-by`.
- **GraphQL `mentions(filter)`, `mentionedBy(filter)`, `mentionIds`, `mentionedByIds`** on `Nib`, plus `mentionsId` / `mentionedById` filter fields on `NibFilter`.
- **GraphQL `Config.prefix`** exposes the configured nib ID prefix to the web UI, enabling correct short-form mention resolution for projects that use non-default prefixes.
- **Web UI: clickable `#<id>` mentions** — mentions in the detail panel body are rewritten into clickable anchors that navigate via the existing `onnibselect` flow. New Mentions and Mentioned-by sections in the detail panel alongside Parent / Children / Blocking.
- **TUI: block move** — when one or more rows are SPACE-selected, Ctrl-Up / Ctrl-Down move them as a contiguous block. Silent no-op when the selection isn't block-movable (non-contiguous, multi-parent, or at the boundary). Single-item selection still moves that item. Selected parent pulls its whole subtree along; descendants of a selected parent are treated as "along for the ride" and never move independently.
- **Reverse-mention index** in Core plus per-request resolver memoization so mention queries serve from a pre-built index instead of re-parsing bodies on every call. Materially faster on large nib graphs.
- Fuzz target and adversarial benchmark for `ExtractMentionTokens`.

### Changed
- **BREAKING: JSON output shapes consolidated** across `list`, `show`, and the retired `refs --both`. Mutation responses wrap their payload in a `success` / `error` envelope; read responses emit bare payloads. Mention, `blocked_by`, and similar relationship arrays are always present as `[]` when empty — never `null` — so `jq` pipelines can iterate unconditionally.
- **Filter ID normalization unified** — passing a short ID to `--blocked-by` now resolves identically to `--mentions` and peers. Fixes a miss where `BlockedByID` short-form lookups silently returned empty.
- Reverse-mention lookup ordering is now deterministic (sorted by ID across all callers).

### Removed
- **BREAKING: `nibs refs`** — retired. Use `nibs links <id> --rel mentions-out` (outbound), `--rel mentions-in` (inbound), or `--rel neighbours` for the full direct-relationship set. The JSON envelope changes from ad-hoc per-direction keys to the unified `{id, relations: {...}}` shape.
- **BREAKING: `nibs deps`** — retired. Use `nibs links <parent> --rel children --order topo` for the topologically ordered child set. `--cycles`, `--graph`, and the deps-specific output flags are not carried over; use `links --json` plus external tooling for graph visualization.

### Fixed
- `ExtractMentionTokens` preserves word-boundary rules across AST text segments — the goldmark tokenizer can split a run of prose across multiple text nodes, and the adjacency check now consults the original source position rather than the local node string.
- `--active` combined with `--status completed` (or `--status scrapped`) now errors at the CLI layer with a clear "filter always yields empty" validation message, instead of silently returning an empty set.

## v0.2.3 - 2026-04-12

### Changed
- Bug nibs are now at the same hierarchy level as features. Bugs can have task and research children, and their valid parents are milestone and epic (no longer feature). This allows breaking down bug fixes into subtasks.

### Fixed
- Resolved all SA5011 staticcheck warnings across the test suite by adding explicit `return` after `t.Fatal` nil guards.

## v0.2.2 - 2026-04-12

### Changed
- Detail panel font sizes now match the list view (inherit base 1rem instead of custom 0.8125rem overrides).
- Detail panel title is now larger (1.25rem) for better visual hierarchy.
- Status, Type, Priority, and Estimate selects now show icons (status dot, type icon, priority indicator, estimate abbreviation) in both the trigger and dropdown items, matching the filter bar style.
- Detail panel metadata fields (Status, Type, Priority, Estimate) are laid out horizontally with wrapping instead of a vertical grid, with small labels above each select.
- Editor modal metadata fields use the same stacked-label layout.
- Detail panel max width increased from a fixed 800px to 75% of the container width.
- Toolbar and filter bar merged into a single row: "New" button is now blue with text, keyword search is always visible, filter dropdowns are inline, and view controls are separated by a divider on the right.

### Fixed
- Keyword search now shows matching child nibs in their full tree hierarchy. Previously, searching in Milestones view would miss matching tasks/features because their parent milestones weren't included in the results.
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

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **`nibs close --as <closed status>`** — closing is now one verb that produces any closed status. `nibs close <id> --summary -` still records `completed`; `--as scrapped` and `--as deferred` record the other reasons. `--as` takes ordinary status names, so there is no separate close-reason vocabulary to keep in sync, and an open status name is a validation error.
- **`## Summary` accrues instead of being replaced.** Each close appends a dated, reason-stamped entry (`**Deferred 2026-07-27** — waiting on the upstream release`), so a nib can be closed again to revise its reason without destroying the first rationale. Re-closing under the same reason is allowed too — that is how a rationale gets updated.
- **The tracking-nib pattern is documented** in `nibs prime` and `nibs prime --full`: when work waits on something outside the tracker, make a nib for the external event and `--blocked-by` it. `--blocked-by` takes ids only, because an external blocker needs a resolution event and only a nib can carry one.
- A guard test parses `web/src/lib/constants.ts` and pins its status names and closed set against the Go configuration, so the web's hand-written copy of the vocabulary can no longer drift unnoticed.

### Changed
- **BREAKING: `deferred` is now a closed status.** Setting work aside is a way of closing it, not a state of being open. `deferred` nibs are excluded from `--open` and `--ready`, included in `-s closed`, and hidden by the web's "Open" preset. A `deferred` blocker still blocks the work that depends on it — unlike `completed`/`scrapped`, the set-aside work is coming back, so the dependency is unmet. **No data migration:** `status: deferred` stays valid in front matter and no file is rewritten.
- **BREAKING: `nibs set -s <a closed status>` is refused**, and the error names the `close` command to run instead. `close` requires a summary and `set` does not, so allowing both made the route that records nothing the shorter one. This governs the `set` verb, not the data — the web UI, TUI, `nibs graphql` and `nibs new -s <closed>` can still reach a closed status directly. Only the move *into* closed is refused; `nibs set <id> -s todo` on a closed nib is still how work comes back.
- **BREAKING: the projected `ready` field now requires a startable status.** It previously meant "not closed and unblocked", so draft and in-progress nibs reported `ready: true` while `nibs list --ready` withheld them — 67 versus 38 on the sample fixture. Both now derive from one `Startable` flag (true for `todo` alone) and answer identically.
- **BREAKING: `nibs plan --open` now selects the open status group**, the same set `list`'s and `rel`'s `--open` select, rather than "everything not closed". The two differ only on a nib whose front matter has no `status:`, which is in neither group and is no longer returned.
- **BREAKING: the paired presence filters collapsed to one field each.** The GraphQL `noParent`, `noBlocking` and `noBlockedBy` filter inputs are retired; `hasParent`, `hasBlocking` and `hasBlockedBy` are tri-state and express all of it. The `--no-parent` and `--no-blocking` CLI flags are unchanged.
- **BREAKING (web):** the `TERMINAL_STATUSES` constant is now `CLOSED_STATUSES` and includes `deferred`, and the Status facet's two presets collapse into a single **Open** — with `deferred` closed, "Open" and "Open + deferred" named the same set. All six statuses remain individually selectable.
- Closing a child now propagates to its parent **by reason**: `## Key Decisions` merge upward for every close reason, but only a `completed` close rewrites the parent's `## Current Focus`. Setting work aside is not progress, and overwriting that section would erase the record of the last real progress. A revision does not retract an earlier `completed` close's parent line — correct it by hand with `nibs body <parent> --section "## Current Focus" --set -`.
- `nibs archive` now moves `deferred` nibs too — it archives every nib in a closed status, and `deferred` is one. Archived nibs remain visible in all queries, as before.

### Removed
- **BREAKING: the `parked` status group** (`-s parked`, `--no-status parked`). It was a one-member group and a spelling variant of `-s deferred`; use `-s deferred`, or `-s closed` for every closed status.
- **BREAKING: the `--active` flag** on `list` and `rel`. `--open` remains, and `-s in-progress` covers the other reading. With `open` meaning draft/todo/in-progress, "active" was wrong in both directions.

### Fixed
- **`nibs check --fix` deleted valid links.** A `blocked_by` or `parent` entry written in short form (`e001` rather than `nibs-e001`) was reported as a broken link and removed from the file on disk, after which the nib was handed out as ready work. Such links now resolve the same way everywhere, and a short-form self-reference is reported as a self link rather than a broken one.
- **`--has-parent=false` and `--has-blocking=false` were silently ignored**, returning the unfiltered set — all 89 nibs on the sample fixture instead of the 24 parentless ones — because the flag's value was tested rather than whether it was given. `--is-blocked=false` had the same defect.
- **Contradictory filter flags returned an empty set with exit 0 and no error.** `--has-parent --no-parent`, `--has-blocking --no-blocking` and `--parent <id> --no-parent` are now validation errors naming both flags.
- **An explicitly empty id filter is no longer ignored.** `--parent ""`, `--mentions ""` and `--mentioned-by ""` returned every nib; they now error. `-S ""` is deliberately still accepted as "no keyword filter".
- The projected `ready` field and `nibs list --ready` disagreed when a nib's `blocked_by` named its blocker by short id — the field withheld it, the filter handed it out.
- Re-closing a nib appended a duplicate copy of its `## Key Decisions` to the parent on every close.
- The `## Summary` date stamp used the machine's local timezone while `updated_at` is UTC, so one close could be recorded under two different dates.
- The duplicate-title warning lost its `reason` for anything closed through `close --as`, because it read only the retired `## Reasons for Scrapping` section. It now reads the most recent `## Summary` entry, falls back to the old section for nibs closed before this change, and explains deferred nibs as well as scrapped ones.
- `nibs list --ready` widened to every unblocked nib, of any status, if no status declared itself startable; that configuration is now a validation error rather than a silently wider answer.
- `nibs catalog`'s `--ready` description no longer disagrees with `nibs list --help`, and the agent guides no longer describe the parent propagation or the closing rules in terms this release makes false.

## v0.7.0 - 2026-07-25

### Added
- **Flat (no-hierarchy) view** for the web table, alongside the tree and grouping views.
- **Created and Modified date columns** in the web table.
- **Sort the web table by clicking any column header** — cycles ascending → descending → off, in every view: a flat sorted list in the Flat view, and sibling-preserving sort (siblings, group buckets, and promoted group headers reorder while nesting stays intact) in the tree and grouping (milestones / epics / features) views.
- **Reorder table columns by dragging a column header**, persisted per view mode.

### Changed
- The **whole column header is now the sort control** — click anywhere in the header, or focus it and press Enter/Space, not just the label text.
- Dragging a column header now shows a **ghost of the header that follows the cursor** and a **no-drop cursor** over places a column can't be dropped, matching the row-drag affordances.

### Fixed
- Dragging a column header no longer selects the header's text.
- The web table no longer momentarily unmounts — interrupting an in-progress column drag or resize — when a background data refresh arrives.
- A column drag no longer gets stuck if the pointer is released outside the window or the tab loses focus.

## v0.6.2 - 2026-07-22

### Changed
- The web UI's **"blocking" indicator now uses the same pill treatment as "blocked"** — a nib that blocks others shows an amber "Blocking" pill (link icon) in the tree and the detail-panel header, honoring the same emphasis setting (subtle icon / pill / dimmed row) as the red "Blocked" pill. New WCAG-AA color tokens back the amber tint in both the dark and light themes.
- **Nib files are now written atomically** — each write goes to a temporary file that is renamed into place, so a crash or a process reading a nib mid-write no longer observes a torn or half-written file.

### Fixed
- **Same-machine concurrent writes no longer silently overwrite each other** — every write operation briefly takes an advisory lock on the data directory, closing a cross-process window where two nibs processes (or `nibs serve` alongside a CLI) could both pass the etag check and clobber one another's write.

## v0.6.1 - 2026-07-20

### Fixed
- GitHub Release notes are populated again — v0.6.0's release body came up empty because GoReleaser's `changelog.disable` suppressed the notes passed to `--release-notes`; removing it restores the changelog-derived body.

### Changed
- The **Release** workflow now refuses to run unless it is dispatched from `main` and the latest CI run for the release commit completed successfully, so a release can no longer be tagged from a red or unverified commit (applies to pre-releases too).

## v0.6.0 - 2026-07-19

### Added
- **Update notifications across CLI, TUI, and web** — nibs checks GitHub for a newer release (cached ~24 h in the user cache directory; silently skipped for dev builds, in CI, or when `NIBS_NO_UPDATE_CHECK` is set) and surfaces it unobtrusively: a trailing line on stderr after an interactive CLI command (never for `--json`, pipes, or `serve`/`graphql`/`query`), a footer hint in the TUI, and a dismissible banner in the web UI — backed by a new best-effort `updateStatus` GraphQL query and remembered per version. Version comparison only: no telemetry and no automatic action.
- **`nibs upgrade`** — downloads, checksum-verifies, and replaces the running binary with the latest release (or a specific `--version <tag>`), rolling back automatically on failure. `--check` reports whether an update is available without changing anything; pre-releases are skipped by default. When nibs was installed by a package manager (Homebrew, Nix, Scoop, Chocolatey, WinGet, or `go install`), `upgrade` defers to that manager with guidance instead of replacing the binary itself.

### Changed
- **BREAKING:** `deferred` is now a nib *status* (parked; non-terminal; excluded from `--ready`) instead of a priority. The priority axis is now `critical / high / normal / low`, and `--priority deferred` is no longer accepted. Existing nibs with `priority: deferred` are normalized to `priority: low` on load — its lowest-rank equivalent — and the normalization is persisted immediately so on-disk and in-memory values agree.
- Release archives are now named `{project}_{os}_{arch}` (the version is no longer embedded in the asset name) and their checksums are published as a single `checksums.txt`, so `nibs upgrade` can match the asset for the running platform; `install.sh` and `install.ps1` resolve the new names.

### Fixed
- Keyword search (web keyword box, `nibs list -S`, GraphQL `search:`) now matches nib IDs and ID fragments directly: a substring of the short ID (very short fragments excluded), a prefix of the full ID, or an exact full ID, case-insensitive, surrounding whitespace trimmed. In queries without an explicit sort (e.g. `nibs query`), ID matches come first (sorted by ID, capped at the search limit), followed by full-text hits in relevance order; sorted surfaces (`nibs list`, the web view) interleave them per their sort. A nib matching both appears once. Previously an ID query returned nothing because the index stores `id` as an unanalyzed keyword field.

## v0.5.0 - 2026-05-06

### Added
- **`--columns` flag on `nibs list` and `nibs links`** — tab-separated tabular output with selectable fields. Comma-separated column names from a closed set: `id, slug, title, status, type, priority, estimate, order, parent, tags, created_at, updated_at`. Output is flat (one row per nib, no per-rel section headers — `links` implies `--flat` semantics: a single deduped row list across all requested rels). Mutually exclusive with `--json`; `nibs list --columns` is also mutually exclusive with `--quiet`. Empty fields render as the empty string; multi-value tag fields are comma-joined; time fields render as RFC3339 (or empty for nil).
- `nibs plan --with-order` flag to display child order keys; `order` field now always present in `nibs plan --json` output.

### Changed
- **BREAKING:** `nibs links --rel children --order topo` now derives edges from each nib's `blocked_by` front-matter field only. Previously it also treated `#<id>` mentions in bodies as ordering edges, which contradicted the documented "mentions are informational only" contract and created spurious cycles when siblings cross-referenced each other for context. Callers that relied on `#<id>` mentions implicitly producing topo ordering must now declare those dependencies via `blocked_by`.

### Fixed
- `nibs --json <cmd>` now produces a single parseable JSON document on stdout for both success and error paths. Cobra's usage block is suppressed on every RunE error (text mode too — usage was meant for `--help` only); in JSON mode the duplicate stderr `Error:` line is also suppressed via a sentinel error. Previously, merged `2>&1 | jq` consumers saw broken JSON, and `nibs <cmd>` errors printed the full flag listing to stderr.
- `nibs create --after`/`--before`/`--first` now work for root-level nibs. Previously they errored with `"positioning requires a parent"`.

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

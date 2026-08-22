# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- **`nibs --version`** alongside the `version` subcommand, printing the same build identity.
- **`nibs check` names illegal parent nests** — each parent/type combination the hierarchy rules refuse is reported with the file, both types and the allowed parents; `--fix` refuses, since re-parenting is not provable intent.
- **The three axis keys `milestone:`, `milestone_order:` and `area:` are modeled front-matter keys** — `milestone:` resolves and re-resolves like `parent:`, a mistyped spelling such as `milestone-order:` is named by `nibs check`, and a malformed value now fails parse instead of hiding among unknown keys.
- **Work is assigned to a milestone and ordered within its queue** — `nibs set --milestone` assigns (never a nib and one of its ancestors both), `nibs list --milestone`/`--backlog` read a queue and the unscheduled remainder, and `nibs mv --queue` repositions one entry by rewriting one file.
- **`nibs next`** — the first startable nib the active milestone's queue leads to, shown with the path it was reached by, exiting 0 whether or not it found one.
- **Completing or scrapping a milestone is refused while open work is still assigned to it** — `--move-open-to <milestone>` or `--unassign-open` dispose of the queue, deferring keeps it, and `nibs check` names a milestone already closed over a live one.

### Changed
- **BREAKING: Epic tops the work tree** — epics and milestones no longer take parents, nothing parents under a milestone, and a milestone carrying `milestone:` or `area:` is refused on write and named by `nibs check`.
- **BREAKING: Milestones assign work instead of containing it** — `nibs migrate` rewrites a milestone's children into `milestone:` assignments ordered by `milestone_order:` and stamps `version: 2`; a milestone now honestly reports 0 children while its progress and queues roll over the assignees.
- **BREAKING: The store carries its own config and keeps active nibs in `data/`** — `.nibs/config.yml`, `.nibs/data/` and `.nibs/archive/` replace a project-root `.nibs.yml` and nib files at the store root, and the `nibs.path` key is retired. Run `nibs migrate` to convert a project; every command refuses until it has.
- **Store-format migrations are now explicit** — `nibs migrate` previews and applies them, and every other command refuses an unmigrated (or newer-format) store instead of silently rewriting files at load.
- **A `.nibs` symlink must lead to a directory that is already a store** — a link at anything else is refused rather than adopted as the project's store, and `nibs init` will not create one through a link. A store reached that way needs the manual steps the refusal prints before `nibs migrate` will run; to keep nibs out of the code repository, initialize the directory with `--nibs-path` (and `--prefix`, since a store outside the project derives its prefix from its own parent) and link `.nibs` at it afterwards.

### Fixed
- **A named pipe or device named like a nib file no longer hangs every command** — it is skipped at load and reported by `nibs check`, like any other file that cannot be read.
- **A store full of unreadable or duplicate nib files no longer floods every command — or a running `nibs serve` — with warnings**, printing at most 20 and then a count pointing at `nibs check`, which still reports every one.
- **A running `nibs serve` now reports a second file claiming a nib id it already holds**, instead of silently answering with whichever of the two the filesystem happened to deliver last.
- **A pre-layout project nested under a store no longer resolves to that ancestor store**, where `nibs migrate` moved and rewrote the ancestor's nib files while leaving the project it was run in untouched.
- **Text read from a file can no longer rewrite the terminal**, such as a control sequence in a config value or a nib body reaching a message that echoes it.
- **Commands a refusal prescribes are quoted for the shell of the platform printing them**, instead of POSIX quoting that `cmd.exe` passes through literally.
- **`nibs prime` no longer primes an agent for a project it cannot use** — a pre-layout project now gets the same migration refusal as every other command, in place of instructions whose every command refuses there.
- **A store refusal no longer denies that any config names the directory** when one names it under a different spelling, which sent the reader to `nibs init` over the project's real nibs.
- **`nibs config set-prefix` renames each nib through an atomic write**, instead of a shared temp name with no fsync.
- **A backtick in a config value can no longer break out of the code span a refusal renders it in**, where the rest of the value became prose addressed to the reader carrying a command of its own choosing.
- **A config file that is not a regular file is refused**, instead of a named pipe or socket at that path blocking the command indefinitely.
- **The web UI now notices a live connection that dies without closing**, such as going offline or waking from sleep, instead of holding an apparently-healthy socket and serving a stale view indefinitely.
- **The server now notices a live-updates client that vanishes without closing** and reclaims its connection, instead of holding it until the OS TCP timeout.
- **Bulk status and priority changes from the web table now carry the same concurrency guard as the detail panel**, instead of silently overwriting a nib that changed after the table loaded it.
- **A store config that omits `default_type` now creates tasks**, matching what `nibs init` writes, instead of milestones.
- **`nibs web` no longer prints an "update available" line into its own startup output** — the entry meant to suppress it named the `serve` alias, which the lookup never sees.
- **`nibs help` and shell completion now work outside a project**, instead of failing with "no .nibs directory found" — which also left `nibs <TAB>` offering filenames in place of subcommands.
- **`nibs config set-prefix` now edits only the prefix key of your config file**, instead of rewriting it from the merged read model — which discarded the file's comments, baked user-level settings into the project, and dropped any key this build does not model.
- **The roadmap's Unscheduled group now shows exactly the work outside every milestone** — items under a milestone hidden by a `--status` filter no longer leak into the backlog, and a work item whose parent link names no nib now appears there instead of vanishing from the roadmap entirely.
- **`nibs new` no longer silently shadows an existing nib when a generated id collides** — a colliding draw is redrawn, and a caller-supplied duplicate id is refused as a conflict instead of leaving two files claiming one id.

## v0.8.3 - 2026-08-13

### Fixed
- **The web table now explains why a row will not drag** — a sort, a search, or the Flat view suppresses reorder, and attempting a drag names the one in effect with a one-click action to clear it.
- **Toasts are set in the app's font**, instead of falling back to the platform's UI font.
- **The web UI reconnects its live updates after the connection drops**, instead of going silently stale until a reload, and shows a "Reconnecting…" indicator while it is down.

## v0.8.2 - 2026-08-10

### Fixed
- **The web UI's scrollbars and other native controls follow the active theme**, instead of rendering as light-mode widgets on the dark palettes in Chromium-based browsers such as Edge.
- **`nibs cheat` lists `context`, `plan` and `roadmap` as the commands they are**, instead of grouping them under a `recipes` label that read as a command but could not be run.

## v0.8.1 - 2026-08-09

### Added
- **The web UI carries the Nibs logo** — a banner in the header in place of the plain title, and a favicon in the browser tab.

### Changed
- **The web UI is set in Mona Sans, bundled with the app**, so text no longer renders differently between browsers depending on which weights the system font happens to ship.
- **The TUI tells the three closed statuses apart** — `deferred` is magenta, and `completed` and `scrapped` sit at different points on a gray ramp, where all three previously shared one gray.

## v0.8.0 - 2026-08-09

### Added
- **A GitHub-style query language in the web filter box** — `field:value` tokens alongside full-text search, covering every field plus relationship and hierarchy predicates, with syntax highlighting, autocomplete, a built-in reference, two-way sync with the facet dropdowns, and a shareable `?q=` URL.
- **"Filter related" on the row context menu** — filter to a nib's blockers, children, ancestors, siblings or mentions without knowing or typing its id.
- **`nibs check` now reports unparseable nib files and duplicate ids on disk**, which were written only to stderr before and so invisible to `--json`, `nibs serve` and the TUI.
- **`nibs close --as <closed status>`** — `--as scrapped` and `--as deferred` alongside the default `completed`.
- **`## Summary` accrues instead of being replaced**, so a nib can be closed again to revise its reason without destroying the first.
- **`storedParentId` (GraphQL) and `-f stored_parent` (CLI)** — the parent link exactly as written on disk, so a link naming no nib stays visible and diagnosable.
- **GraphQL `NibFilter` gains `ancestorId`, `descendantId` and `siblingId`**, each excluding the target nib itself.
- **The GraphQL schema now documents that a batch mutation is not atomic**, and names the wire error codes `nibs serve` can return, including `ETAG_MISMATCH`.
- **The tracking-nib pattern is documented** in `nibs prime`: when work waits on something outside the tracker, make a nib for the external event and `--blocked-by` it.
- **A web setting for which click opens a nib** (Settings → Behavior): with "Double click" selected, a single click selects a row without opening the detail panel.

### Changed
- **BREAKING: `parentId` reports the resolved parent, not the raw stored link.** A `parent:` naming no nib now reports `parentId: null`, matching `parent`, `hasParent` and `siblingId`; the raw link moved to `storedParentId` / `-f stored_parent`.
- **BREAKING: `deferred` is now a closed status** — excluded from `--open` and `--ready`, included in `-s closed`, and hidden by the web's "Open" preset, though a `deferred` blocker still blocks. No data migration: `status: deferred` stays valid in front matter and no file is rewritten.
- **BREAKING: `nibs set -s <a closed status>` is refused**, and the error names `close` instead. Only the move *into* closed is governed; `nibs set <id> -s todo` is still how work comes back.
- **BREAKING: the projected `ready` field now requires a startable status** (`todo` alone), so it and `nibs list --ready` answer identically.
- **BREAKING: `nibs plan --open` selects the open status group**, the same set `list` and `rel` select, rather than "everything not closed".
- **BREAKING: the paired presence filters collapsed to one field each.** The GraphQL `noParent`, `noBlocking` and `noBlockedBy` inputs are retired in favor of the tri-state `hasParent`, `hasBlocking` and `hasBlockedBy`; the `--no-parent` / `--no-blocking` CLI flags are unchanged.
- **BREAKING: a filter naming a nib that does not exist is refused instead of answered with an empty list** — `nibs list --parent <unknown-id>` exits 3, and the equivalent GraphQL query returns an error; a target that exists and simply matches nothing is unchanged.
- **BREAKING (web): the Status facet's two presets collapse into a single "Open"**, with all six statuses still individually selectable.
- Closing a child now propagates to its parent **by reason**: `## Key Decisions` merge upward for every close reason, but only a `completed` close rewrites the parent's `## Current Focus`.
- `nibs archive` now moves `deferred` nibs too — it archives every nib in a closed status.
- The web UI settled on one name per field: **Status** (previously "State" in places) and **Estimate** (previously "Effort"), matching the words the query language and the CLI use.
- The status pickers in the TUI and the web now list statuses in transition order, while lists and the status column keep sorting most-active-first.
- **The web table distinguishes the row the detail panel is showing from the rows a bulk action would affect** — the background fill marks the selection set alone, an amber leading bar marks the open row under the double-click preference, and every row exposes `aria-selected`.
- Deleting or archiving from the web table now closes the detail panel only when it removed the nib the panel was showing, instead of closing it every time.
- `nibs serve` sends SPA cache headers, so a rebuilt UI is picked up instead of being served stale from the browser cache.
- The TUI's Charm stack (Bubble Tea, Bubbles, Lip Gloss, Glamour) moved to v2 with no intended behavior change.
- The web dependency tree moved forward: dompurify, the Svelte runtime, bits-ui, CodeMirror, Tailwind, Playwright and `@lucide/svelte`, plus `tinykeys` 4, `marked` 18 and `@testing-library/jest-dom` 7.
- The Go dependency tree moved forward: bleve 2.6, gqlgen 0.17.94, goldmark 1.8.5, fsnotify 1.10 and the `golang.org/x` packages.

### Removed
- **BREAKING: the `parked` status group** (`-s parked`, `--no-status parked`) — a one-member group and a spelling variant of `-s deferred`; use `-s deferred`, or `-s closed` for every closed status.
- **BREAKING: the `--active` flag** on `list` and `rel` — `--open` remains, and `-s in-progress` covers the other reading.

### Fixed
- **A nib whose `parent:` names a nib that does not exist is now a root everywhere**, instead of being a root to some surfaces and parented to others; `nibs check` still reports the link as broken.
- The knock-on effects of that bug went with it: changing such a nib's type no longer fails, `--clear parent` no longer sends it to the end of the root order, root-level bulk reorder works again in a project holding one, and the TUI can move it among the roots it is displayed with.
- **BREAKING: a parent cycle renders instead of erasing every nib in it** — the lowest-id member is promoted to a root and the rest nest beneath it.
- **BREAKING: a `search` term now filters relationship fields, where it was silently dropped** — `children(filter:{search:"foo"})` returned every child regardless of the term.
- **BREAKING: two unsatisfiable filter combinations are refused instead of answered with an empty list** — `parentId` with `hasParent: false`, and `blockedById` with `hasBlockedBy: false`. `blockingId` with `hasBlocking: false` is *not* refused: it selects the blockers a nib still lists that no longer block anything.
- **BREAKING: an explicitly empty id-valued filter value is refused instead of being ignored.** `nibs list --parent ""` returned the whole store; all eight id-valued fields now report a validation error, while an empty `search` still means "no keyword filter".
- **BREAKING: `nibs query` reports the same exit codes as the direct commands** — not-found 3, an unreadable target 5, a stale `if-match` 4 with the server's `currentEtag`. A response whose errors span different exit classes reports the new `UNCATEGORIZED` code and exits 1.
- `nibs mv <unknown-id>` exits 3, like every other command.
- **On Windows, the TUI and the web UI now pick up external nib changes**, where the watcher previously discarded the whole batch.
- **`nibs check --fix` deleted valid links.** A `blocked_by` or `parent` entry written in short form (`e001` rather than `nibs-e001`) was reported as broken and removed from the file on disk, after which the nib was handed out as ready work.
- **A link written in short form is now traversable from both ends** — it answered from the nib holding it, but the nib it named listed no such child.
- `nibs check --fix` no longer removes links whose target file merely failed to parse.
- **A refused `updateNib` no longer leaves durable edits on other nibs** — the subject's write-free guards now run ahead of every step that can touch another nib.
- **A failing batch mutation names what it committed** — a batch could delete two nibs, fail on a third, and report a bare "nib not found" naming neither deletion.
- **`deleteNib` deletes and unlinks the same nib**, where it removed the target while stripping zero incoming links; `nibs rm` and `nibs delete` were never affected.
- **A stale etag on a bulk reorder is now a retryable conflict** — `ETAG_MISMATCH` on the wire and exit 4 with the server's current etag, rather than a generic IO failure — and both refusal paths name the nib they refused on.
- Bulk-reorder errors reported a parent that was not the one compared; the message now names the resolved value, and the stored spelling alongside it when the two differ.
- **`--has-parent=false`, `--has-blocking=false` and `--is-blocked=false` were silently ignored**, returning the unfiltered set.
- **Contradictory filter flags returned an empty set with exit 0** — `--has-parent --no-parent`, `--has-blocking --no-blocking` and `--parent <id> --no-parent` are now validation errors naming both flags.
- **`nibs rel <id>` silently defaulted to `--rel neighbours`** without saying so in the help, the flag usage or `nibs cheat`.
- **`nibs set --parent` and `nibs query` report an illegal reparent as a hierarchy error**, with the `allowedParentTypes` hint `nibs mv` and `nibs new` already gave, and a `--type` change that would orphan a child now names which child.
- **`nibs query` reports the `occurrences` count when a surgical replace is refused** — `TEXT_NOT_FOUND` or `TEXT_AMBIGUOUS` with the count.
- **An oversized filter id no longer amplifies the response by the size of the store** — the echo is capped, with the original length reported.
- A search result could repeat an ancestor when two nibs named the same parent with different spellings of its id.
- **The font-size preference now reaches the filter box, buttons and dropdown menus**, which stayed pinned at one size while the table and detail text moved.
- Every refused reorder in the TUI now says why — nibs under different parents, rows that are not adjacent, or the end of a list — instead of doing nothing at all.
- Refusals and failures in the TUI footer no longer render in the same green as a success.
- **Pre-release versions are marked as pre-releases on GitHub**, so `nibs upgrade` no longer offers them as routine stable upgrades.

### Security
- **BREAKING: `nibs upgrade` now requires releases to be signed.** It verifies `checksums.txt` against an Ed25519 key compiled into the binary, then the archive against that file; because the signature is fetched during release *detection*, `nibs upgrade --version <tag>` cannot reach any release predating signing, though upgrading *to* the first signed release is unaffected.
- **Releases publish `checksums.txt.sig`**, a detached Ed25519 signature over the checksum file produced from a key held in a protected `release` environment, with three public keys compiled into every binary so the signing key can be rotated later without stranding installs.
- **Release assets carry a build provenance attestation** — verifiable with `gh attestation verify nibs_linux_amd64.tar.gz --repo alphaleonis/nibs`, anchored in Sigstore and GitHub's OIDC identity rather than a key the project holds.
- **Every input to the release job is pinned** — GoReleaser and `go-licenses` to exact versions, and every `uses:` reference to a full commit SHA — so no arbitrary upstream release can run as code in a job holding `contents: write`.
- **CI now fails on a dependency with a known vulnerability.** `task vulncheck` (govulncheck) and `task web:vulncheck` (`npm audit`) run in the lint job and on a weekly schedule, each gated by an explicit allowlist that fails both on an unreviewed advisory and on an entry that no longer matches anything.
- **Two vulnerable Go dependencies reachable from nibs' own code were updated**: goldmark, closing a cross-site scripting issue ([GO-2026-5320](https://pkg.go.dev/vuln/GO-2026-5320)) reachable from mention extraction, and `golang.org/x/text`, closing an infinite loop on invalid UTF-8 ([GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970)).

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

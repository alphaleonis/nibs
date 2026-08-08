# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Nibs

Nibs is a file-based issue tracker for AI-first workflows. Issues ("nibs") are stored as Markdown files with YAML front matter in a `.nibs/` directory. It provides a CLI, a Bubbletea TUI, and a built-in GraphQL query engine that coding agents use to interact with nibs.

## Build & Dev Commands

All commands use [Task](https://taskfile.dev/) (`go-task/task`) as the task runner. Go, golangci-lint, Task, and Node are pinned in `mise.toml` (Node to a major, the rest to exact versions), and [mise](https://mise.jdx.dev/) is the only thing to install by hand — but installing mise is not enough on its own. A fresh checkout needs `mise trust` once — mise refuses to read a `mise.toml` it has not been told to trust, and while a terminal offers to trust it interactively, a non-interactive run (CI, a pre-commit hook, a coding agent) only sees the refusal. Then run `mise install` and make mise's tools reachable, either by putting its shims directory on PATH (`~/.local/share/mise/shims`; `%LOCALAPPDATA%\mise\shims` — [Unverified] on Windows) or by activating mise in your shell profile. Note that `mise activate <shell>` only *prints* the activation script, so it has to be eval'd: `eval "$(mise activate zsh)"`, and see [mise's getting-started guide](https://mise.jdx.dev/getting-started.html) for other shells. Skip the reachability step and `go`, `task` and `node` silently resolve to whatever is installed system-wide rather than the pinned versions, while golangci-lint — which normally exists only inside mise — is absent entirely. `task lint` is the exception: it invokes `mise exec -- golangci-lint`, so it uses the pin whether or not the shims are on PATH, and it fails with a guided message when mise is missing or mise cannot load this checkout's config (usually because it is untrusted). It also fetches the pinned golangci-lint over the network on first use if `mise install` has not already cached it — run `mise install` ahead of time in network-restricted environments.

CI reads those same pins from `mise.toml`: both workflows provision their tools with `jdx/mise-action`, so `mise.toml` is the only place any of these versions appear and a bump there reaches CI with no second edit. Still re-run `task lint` after moving golangci-lint — a new release can add or retire checks and move the clean baseline.

- `task build` - Build the `./nibs` executable (runs codegen first)
- `task test` - Run all tests: Go + web (runs codegen and web:build first); includes the `-race` gate on `internal/nibcore` + `internal/graph`, and both web type-check gates via `task web:check`
- `task check` / `task web:check` - Type-check the web UI. Two passes over two disjoint file sets: `svelte-check` against `web/tsconfig.json` for `src/`, then `tsc` against `web/tsconfig.tooling.json` for everything outside it (the Playwright suites in `screenshots/` and `e2e/`, plus the top-level vite/vitest/playwright/codegen configs). The split exists because the second set runs in Node and needs `types: ["node"]`, which would leak Node's globals into browser code if merged into the app config. CI's lint job calls `task web:check`, so both passes gate CI as well — its test job deliberately does not go through `task test`.
- `task test:race` - Run the `-race` detector on the concurrency-critical packages (`internal/nibcore` + `internal/graph`) only
- `task codegen` - Regenerate GraphQL code: Go server (gqlgen, `go generate ./...`) + web client types (graphql-codegen client preset). Runs `go:codegen` + `web:codegen`.
- `task nibs` - Build and run the CLI in one step (`go run .`)
- `go test ./internal/nib/` - Run tests for a specific package
- `task web:install && cd web && bash ../scripts/run-capped.sh npx vitest run --reporter=agent` - Run web tests only
- `task demo` - Serve the web UI with the sample-project fixture (temporary copy, safe to mutate)
- `task demo:tui` - Run the TUI with the sample-project fixture
- `task screenshots` - Capture web UI screenshots to `web/screenshots/output/` for visual verification
- `task --list` - List all available tasks

`go build ./...` fails in a fresh git worktree — `embed.go` embeds `web/dist`, which is gitignored, so it looks like a broken checkout (`pattern all:web/dist: no matching files found`). Build the web assets there first, or scope to what you need (`go test ./internal/...`).

## GraphQL Schema Changes

When modifying the GraphQL schema (`internal/graph/schema.graphqls`):

1. Edit the schema file
2. Run `task codegen` to regenerate `internal/graph/generated.go` and `internal/graph/model/models_gen.go`
3. Implement any new resolvers in `internal/graph/schema.resolvers.go`
4. If you changed the `NibFilter` input, mirror the field in the hand-written `NibFilter` in `web/src/lib/types.ts`. A compile-time guard there asserts the two key sets are **equal in both directions**, so adding or removing a schema field without touching the client type fails `npm run check` — which runs in `task test`, not `task build`.

The code generation is configured in `gqlgen.yml`. The `nib.Nib` struct is autobound so the GraphQL `Nib` type maps directly to it.

**Never put a comment directive on a resolver's doc comment.** gqlgen rewrites `internal/graph/schema.resolvers.go` on every codegen, and the pinned version (see `go.mod`) rebuilds each resolver's doc comment through `go/ast`'s `CommentGroup.Text()`, which discards comment directives — `//nolint:…`, `//go:noinline`, the `//<tool>:<directive>` form of `go/ast`'s `isDirective` rule — while keeping the prose around them. The directive is gone on the next `task codegen` and whatever it was doing silently stops. Resolver *bodies* are copied through as raw source, so put the directive **inside the function**. `task go:codegen` greps for one before running gqlgen and fails the build, but that check is best-effort: it is skipped on a machine without `grep` on PATH, and on a resolver file it cannot read (a rename or a `resolver.layout` change degrades it to a no-op rather than blocking codegen).

Which parts of that file survive a regeneration and which do not is written up in full at the top of `internal/graph/resolver.go` — read it before assuming a note will last in `schema.resolvers.go`. In short: resolver bodies, resolver doc-comment prose and the import block survive; a free-standing comment and a non-resolver declaration do not. gqlgen v0.17.86 fixes the directive behavior upstream, and `task go:codegen` fails with removal instructions once `go.mod` reaches that version.

## Architecture

### Data Flow: CLI -> GraphQL -> Core -> Filesystem

All CLI commands that interact with nibs go through GraphQL internally. The flow is:

```
cmd/*.go (Cobra commands)
  -> internal/graph/ (GraphQL resolvers, executed in-process via gqlgen executor)
    -> internal/nibcore/ (thread-safe in-memory store with disk persistence)
      -> internal/nib/ (Nib struct, parsing, rendering of Markdown+YAML files)
```

The GraphQL engine runs in-process for CLI commands (`cmd/graphql.go` executes queries via `executor.New(es)`). It is also exposed over HTTP by `cmd/serve.go` (`nibs serve`).

### Key Packages

- **`cmd/`** - Cobra CLI commands. Each file is a command (create, list, show, update, delete, archive, graphql, tui, prime, etc.)
- **`internal/nib/`** - Core `Nib` struct, Markdown+YAML parsing/rendering, ID generation, sorting
- **`internal/nibcore/`** - `Core` type: thread-safe in-memory nib store with filesystem persistence, file watching (fsnotify), search (Bleve), and archive management
- **`internal/graph/`** - gqlgen GraphQL layer. `schema.graphqls` is the schema, `schema.resolvers.go` has resolver implementations, `resolver.go` has shared validation logic (parent hierarchy, cycle detection, etag validation)
- **`internal/config/`** - Configuration from `.nibs.yml`. Hardcoded enums: statuses (draft/todo/in-progress/deferred/completed/scrapped), types (milestone/epic/bug/feature/task), priorities (critical/high/normal/low)
- **`internal/tui/`** - Bubbletea TUI app. Uses the GraphQL resolver internally for all mutations
- **`internal/search/`** - Bleve full-text search index (lazy-initialized, in-memory)
- **`internal/ui/`** - Shared UI utilities (styles, tree rendering)
- **`internal/mdsection/`** - Markdown section parsing and editing (find, replace, append heading-delimited sections)
- **`internal/bodytemplate/`** - Markdown body templates per nib type (task, bug, epic, milestone, research)
- **`internal/output/`** - CLI output formatting

### Web UI (`web/`)

- Svelte 5 (runes), Vite, Tailwind CSS v4, shadcn-svelte (component library), urql for GraphQL, Vitest for tests
- shadcn-svelte provides accessible, pre-styled components built on Bits UI. Components are copied into `web/src/lib/components/ui/` and are fully customizable. Use shadcn components for all new UI primitives (dialogs, selects, dropdowns, popovers, buttons, etc.) rather than hand-rolling.
- **Tailwind v4 theming**: Color tokens use a two-layer system. Bare variables in `:root` (e.g., `--warning: oklch(...)`) and Tailwind registrations in `@theme inline` (e.g., `--color-warning: var(--warning)`). Without the `@theme` mapping, utility classes like `bg-warning` resolve to transparent. All new custom colors must be added to both layers.
- **Use shadcn tokens for all UI colors** — never use hardcoded Tailwind color classes (`gray-700`, `blue-500`, etc.) in components. Use semantic tokens: `bg-popover`, `text-muted-foreground`, `border-border`, `bg-accent`, `text-foreground`, etc. Domain-specific tokens (`bg-warning`, `text-link`, `border-tag-border`) are also registered as Tailwind utilities.
- **When migrating to shadcn components**, replace both the container AND the internal items/styling. Don't wrap old hand-rolled markup in shadcn containers — use shadcn's item components (e.g., `DropdownMenu.CheckboxItem` instead of raw `<label><input checkbox>`), which provide consistent padding, hover states, and ARIA roles.
- **Bits UI portals in jsdom**: shadcn components render content via portals to `document.body`. `test-setup.ts` has polyfills (ResizeObserver, MutationObserver visibility fix) that make portaled content queryable. Tests use `screen.*` queries which search the full document. DropdownMenu items use roles like `menuitemcheckbox`, `menuitemradio`, `menuitem`.
- `web/dist/` is gitignored — run `task build` to generate it before `go build`.
- Preferences (filter, view level, column widths, panel width) are persisted to localStorage via `web/src/lib/storage.ts`
- The table uses `table-layout: fixed` with an explicit computed width — column widths are enforced regardless of content

### Web UI Conventions

- **Event delegation**: TreeTable uses delegated event handlers on the scroll container (not per-row callbacks). TreeTableRow is a pure render component with zero callback props — interactive elements use `data-action` attributes (toggle, title, add-child, drag-handle). New actions require a handler case in TreeTable's `handleDelegatedClick`.
- **Svelte context for ambient state**: SelectionState and DragState are provided via `provideSelection`/`provideDrag` from `contexts.ts`. Components read with `useSelection()`/`useDrag()`. Tests must provide context via `makeTestContext()` from contexts.ts — pass as `context` option to `render()`.
- **Shared field components**: Use `StatusSelect`, `TypeSelect`, `PrioritySelect`, `EstimateSelect`, `TagEditor` from `web/src/lib/components/` instead of inline select/tag markup. Use `renderMarkdown()` from `web/src/lib/markdown.ts` instead of inline DOMPurify+marked. Use `.prose-nib` CSS class for markdown prose styling.
- There is no prettier config — **do not run prettier**; it reformats unrelated files (one run churned 129 lines of untouched code).

### Nib Data Model

Nibs are Markdown files with YAML front matter stored in `.nibs/`. Filename format: `{id}-{slug}.md` or `{id}.md`. Archived nibs go to `.nibs/archive/` but **stay in the store and remain visible in all queries** — `Core.Archive` keeps the nib in memory and rewrites its `Path`. Archiving is a move, not a removal (only `Delete` removes). The `Nib` struct fields like `ID`, `Slug`, and `Path` are derived from the filename/path, not from front matter.

The `Path` field always uses forward slashes (normalized via `filepath.ToSlash`) for cross-platform portability. When using `Path` for filesystem operations, combine with `filepath.Join(c.root, b.Path)` which handles mixed separators.

### Configuration

Project config lives in `.nibs.yml` at project root (searched upward from cwd). Key settings: `nibs.prefix` (ID prefix like "myproj-"), `nibs.id_length`, `nibs.path` (data directory, default `.nibs`), `nibs.require_if_match` (optimistic concurrency). Nibs path can also be set via `--nibs-path` flag or `NIBS_PATH` env var — but those move **only the data directory**; config is still discovered from cwd, so pointing them at another project silently applies *this* project's prefix/id_length/defaults to that project's data. To work against another project, pass `--config <dir>/.nibs.yml` — it resolves that project's data directory too, so `--nibs-path` is unnecessary.

For optional config fields with non-zero defaults, use pointer types (`*int`, `*bool`) with `yaml:"...,omitempty"` so nil means "use default" vs explicit zero/false. See `ServerConfig` for the pattern.

### Agent Integration

`nibs prime` outputs the agent onboarding prompt (slim `cmd/prompt.tmpl`, or the full guide `cmd/prompt-full.tmpl` with `--full`). `nibs cheat` prints the whole CLI grammar on one screen, and `nibs catalog <topic>` emits generated vocabulary (fields, filters, hierarchy, recipes, examples, schema). Together these are the primary interface for AI agents.

## Branching & Workflow

- **`develop` is where unreleased work lands.** Cut feature branches from it, merge them back into it. This is the branch to be on for normal development.
- **`main` is the released branch.** `develop` merges into `main` at release time and not before — do not merge feature work into `main` directly.
- We are not using pull requests currently — merge feature branches directly into `develop` and push. Renovate is the exception: it opens PRs against `develop` for the pinned toolchain, and those are reviewed and merged as PRs.
- Create feature branches for non-trivial work, merge when done

### Two epics shipping in one release

Keep them on **separate feature branches and integrate through `develop`** — do not stack one epic's work onto the other's branch. Stacking makes them one inseparable unit that cannot be reviewed, reverted, or reordered at release time, and it hides a cross-cutting change under an unrelated branch name.

When one epic depends on the other, merge `develop` into the dependent feature branch (a merge, not a rebase — rebasing replays every commit against a changed base, and intermediate commits often will not build).

### Write CHANGELOG entries at merge time

Add a feature branch's `[Unreleased]` entries **when merging it into `develop`**, not while the work is in progress. Parallel branches each editing the same `[Unreleased]` section conflict every time, and those conflicts are the expensive kind where both sides are correct.

**What belongs in an entry** — three tests, all three of which the v0.8.0 cycle failed:

- **One sentence.** Two only for a BREAKING change needing a migration note. Rationale, alternatives considered and verification evidence belong in the nib and the commit message.
- **Only what changed since the last release.** A bug introduced *and* fixed inside the same unreleased cycle is not a change — no user saw the round trip. Verify rather than assume the behavior predates the tag: `git show v0.7.0:cmd/close.go`, `git grep <symbol> v0.7.0 -- internal/`.
- **Only what a user of the released binary can observe.** Build/CI/lint/test-infrastructure work stays out, as do "now guarded by a test" entries and anything deliberately *not* done. Release-security changes (signing, provenance, advisory gates) stay in — they govern what users download.

Match the terseness of `v0.7.0` and earlier, not the section above it.

### Sync before starting work

Before starting any new work, run `git fetch` (and `git -C .nibs fetch`) and check whether the local branch is behind its remote. If behind: when the worktree is clean, `git pull --ff-only` (`git -C .nibs pull --rebase` for `.nibs/`); when the worktree is dirty, **stop and ask** — do not auto-stash, auto-rebase, or carry on against a stale base. Skipping this check has burned us: building on top of a stale `main` produced a CHANGELOG entry that collided with an already-released version, plus rework to rebase the change onto the real tip.

## The `.nibs/` Directory Is a Separate Git Repository

`.nibs/` is gitignored in the main repo and is itself an independent git repository with its own remote (`https://github.com/alphaleonis/nibs-nibs.git`).

- **Everything in `.nibs/` goes to `main`** — no feature branches for nib changes, no PRs. Commit and push directly.
- **Fetch, commit, and push `.nibs/` separately** from the main repo. `git status` at the project root will never show changes inside `.nibs/`.
- To operate on it, `cd .nibs` first, or use `git -C .nibs ...`. Do not try to add `.nibs/...` paths from the outer repo — they are ignored.
- When a code change and a nib update belong together conceptually, they still become **two commits in two repos**: one in the outer repo for code, one in `.nibs/` for the nib. Reference the nib ID in the outer commit (`Refs: nibs-xxxx`) as usual; the `.nibs/` commit is typically a short `chore:` message describing the nib change.
- Before starting work, consider `git -C .nibs fetch && git -C .nibs pull` so you see the latest nib state (nibs may have been updated from another machine or by a teammate).

## Commits

- Use conventional commit messages ("feat", "fix", "chore", etc.)
- Include relevant nib ID(s) in commit messages (e.g. `Refs: nib-xxxx`)
- Mark breaking changes with `!` notation (e.g. `feat!: ...`)
- Description should be a concise bullet point list of changes
- **Never commit with failing tests**, even if the failures appear unrelated to your changes. Run `task test` before every commit — but if tests already passed since the last code change and no code has been modified since, skip the redundant run. If tests fail: either fix them in the same commit, or stash your changes (`git stash`), fix the tests and commit the fix separately, then reapply your stash and commit your work. Do not ignore or skip failing tests.
- **Never commit with build warnings.** Run `task build` and check for warnings (deprecation notices, unused imports, etc.) before deeming a work item complete. Treat warnings as errors — fix them before committing.
- **Never commit with lint failures.** Run `task lint` (golangci-lint) before committing. Fix all lint issues before committing.

## Testing

- Always write or update tests for changes
- **NEVER invoke `go test` directly — use `task test`, or `scripts/go-test-capped.sh <args>` for a targeted run.** A bare `go test` runs uncapped in `init.scope`, where a runaway test exhausts RAM and swap and triggers a *global* OOM that tears down the whole WSL VM and SIGKILLs every terminal. This has happened twice (`nibs-mv0i` 2026-07-06, `nibs-mlss` 2026-07-29); both times the capped runner existed and was simply bypassed. This is a WSL failure mode specifically — on native Linux, macOS and Windows the same runaway is an ordinary process-level OOM that kills the test binary and nothing else — so `go-test-capped.sh` caps on WSL and refuses to run there when it cannot (`NIBS_UNCAPPED=1` overrides, deliberately), while off WSL it is a plain `go test`. The capping itself lives in `scripts/run-capped.sh`, shared with the web lanes; `go-test-capped.sh` is the `go test` front end that opts into the strict refusal. The ceiling is `NIBS_CAP_MEM_MAX` (default 4G) — measured headroom before anyone raises it: the `-race` lane peaks at ~222 MB. A `PreToolUse` hook (`scripts/guard-go-test.sh`, a shim over `guard-go-test.py`) rejects bare `go test` in agent Bash calls — but it needs a working Python, and warns and allows the call when it finds none, so do not lean on it as a backstop. **`-timeout` is not a substitute** — it bounds time, not allocation, and a runaway can allocate ~770 MB/s, so the machine dies long before the timeout fires. Pass this rule down to every subagent brief.
- **Prove a new guard bites**: break the behavior it guards and confirm the test fails. A test that passes while its target is broken is decoration. This keeps happening here — guards placed after an assert that throws first (so they never run), and a mutation that survived the entire 1240-test suite. **When the guard protects *termination*, the mutant does not fail an assertion — it allocates without bound**, so run that probe only through the capped runner (an OOM-killed cgroup exits 137 and is itself a valid "the guard bites" signal). A probe killed mid-run leaves its mutation in the working tree: snapshot with `git stash create` first, and after any crash diff against that snapshot *before* re-running anything.
- Use table-driven tests following Go conventions
- Never hardcode `/` or `\` in path assertions — use `filepath.Join` for OS paths and forward slashes for nib `Path` fields
- For manual CLI testing: `task nibs` compiles and runs the CLI
- For manual CLI testing, `task demo` serves the web UI with a temporary copy of the sample-project fixture (safe to mutate), and `task demo:tui` does the same for the TUI
- **Test fixture dataset**: `testdata/fixtures/sample-project/` has 89 curated nibs (prefix `tnib-`) covering all types, statuses, priorities, hierarchies, and relationships. Use `fixtures.CopySampleProject(t)` from `testdata/fixtures/` to get a temporary copy for write tests. Regenerate with `bash testdata/fixtures/gen-sample-project.sh`.
- Web UI tests: `task web:install && cd web && bash ../scripts/run-capped.sh npx vitest run --reporter=agent` (Vitest + jsdom + @testing-library/svelte).
- **The web lanes are memory-capped too, for a different reason than the Go lane.** vitest has never run away; it fans out one jsdom worker per core, and an uncapped fleet is charged to whatever cgroup launched `task test`. When that is a long-lived agent session, the kernel's memcg OOM killer picks the *session* over any single worker — it has days of swap entries and page tables counting toward `oom_badness` — so the supervisor dies and the tests live (`nibs-0kip`, 2026-08-07). `scripts/run-capped.sh` gives each lane its own scope so it pays for its own memory, and `web/vitest.config.ts` pins `maxWorkers: 4` (measured: 2.6 GB, vs >6 GB unbounded on a 24-core box). A lane that exceeds its ceiling exits 137 and fails the gate — it does not pass silently. Unlike the Go lane, the web lanes **run uncapped rather than refusing** where no cap is available: losing the cap costs isolation, not the machine.
- **Never run `npm install`/`npm ci` in `web/` by hand** — use `task web:install`, which reinstalls only when `package-lock.json` changed and repairs a tree npm left broken. A hand-run install overlapping a `task` run corrupts `node_modules` (`nibs-wcx3`), and `npm install` can rewrite `package-lock.json` on top of that.
- **Never run two `task` invocations that install at the same time** — there is no lock, deliberately: `mkdir` is not atomic under uutils coreutils (this machine) and is reportedly a no-op on Windows, so a lock there would only look like protection. Installs are rare (lockfile changes only), so the rule is cheap. This also covers `goreleaser release --snapshot` from `RELEASING.md`, whose `.goreleaser.yaml` `before` hook runs `cd web && npm ci` outside the task.
- **A `task` run canceled by PID may still be running — do not immediately re-run it** (`nibs-uur9`). Signaling Task's PID *alone* does not abort it: Task lets the current command finish, then runs **every remaining command** and exits 0. Signaling the process **group** does abort it — that is how to cancel a `task` run correctly. (PID-alone reproduced 3x on Task v3.50.0 with SIGTERM and SIGINT on Linux/WSL; the group result measured in the same runs. [Unverified] on Windows.) [Inference] On Linux/WSL an interactive Ctrl+C signals the foreground process group, so it lands on the safe side of that distinction; [Unverified] Windows has no POSIX foreground group and its console Ctrl+C is untested here, so run the check below there regardless. The hazard is *programmatic* cancellation — `subprocess.terminate()` and `child_process.kill()` signal the PID by definition, and [Unverified] whether a CI step timeout or an agent harness's Bash-tool timeout does. Before retrying, confirm nothing is still installing: `Get-Process task -ErrorAction SilentlyContinue` (PowerShell) or `pgrep -af '\btask\b'` (Linux/WSL). Look for `task`, not `node` — unrelated `node` processes are always running. A kill that takes Task down outright is the exception: it leaves `npm ci` reparented and invisible to this check, so on a hard kill skip to the `rm -rf` recovery below rather than trusting a clear result. Otherwise, once the check is clear, a plain `task web:install` is enough: if the orphan's install completed, `cmp -s` reports the tree up to date and this is a no-op; if it died first, the tree is unstamped and this reinstalls it. Reach for `rm -rf web/node_modules && task web:install` (`Remove-Item -Recurse -Force web/node_modules` in PowerShell, no other `task` run active) on any known overlap or a tree still broken after `npm rebuild` — not only on visible corruption, since the stamp and `.bin` probes cannot see damage deeper in the tree.
- Web test commands require `web/` as the working directory. If cwd has drifted, `cd` to the project root's `web/` directory first.
- **Always use `--reporter=agent`** when running vitest — it keeps output concise. Never pipe vitest through grep; read the output once.
- `task test` runs both Go and web tests. No need to run them separately unless debugging a specific failure.
- **`-race` runs automatically**: `task test` runs `internal/nibcore` + `internal/graph` a second time under `-race` (via `task test:race`), and CI runs the same detector lane on Linux. These packages carry the concurrency invariants (live-pointer/copy-on-write, clone-under-lock), whose guards degrade to trivial checks without `-race` — so a reintroduced data race now fails the default gate, not just a manual `go test -race`.
- **Visual verification of the web UI**: `task screenshots` captures PNGs of the key UI states (table at each view level, detail panel, editor modal, context menu) into `web/screenshots/output/` (gitignored), served from a temp copy of the sample fixture. Read the PNGs to *see* rendered changes — jsdom tests can't verify pixels. One-time setup: `cd web && npx playwright install chromium` (plain `install`, not `--with-deps` — that flag is apt-only and fails on Fedora). Extend `web/screenshots/capture.spec.ts` when new views or themes land.
- **bits-ui timer flush**: `test-setup.ts` has an `afterAll` that waits 50ms so bits-ui's body-scroll-lock deferred cleanup (24ms setTimeout) fires while jsdom still exists. Without this, the timer fires after jsdom teardown causing a spurious "document is not defined" error.
- **Never open a bits-ui submenu by hovering in jsdom** — use `openSubmenu()` from `web/src/lib/testing/menu.ts`, which opens it by keyboard. bits-ui keeps a hovered submenu open by testing pointer coordinates against `getBoundingClientRect()` corridors and `document.elementFromPoint()`; jsdom reports every rect as 0x0 at the origin and does no hit-testing, so it decides the pointer left and unmounts the submenu — often between the query that finds an item and the click on it, which then dispatches nothing at all and fails with `Number of calls: 0`. This is a jsdom artifact, not an app bug: the hover path works in Chromium and is covered there by `web/e2e/context-menu.test.ts` (`task playwright`). The e2e lane serves a throwaway copy of the sample fixture, so e2e tests may mutate freely.

## Architecture Reviews

Before breaking down a new phase or starting a large feature that touches existing interactive components (especially the web UI), run `/decaf-experimental:improve-codebase-architecture` scoped to the area that phase will touch. This identifies structural issues (prop explosion, god components, tightly coupled interaction systems) *before* new features pile onto them. If the review surfaces refactoring opportunities, create nibs for them and make them prerequisites of the new features.

## When Executing Plans

When working from a plan or work item:

- **Before starting implementation:** check the plan documents (`plans/`) for technology choices (libraries, frameworks, patterns). If the plan specifies a dependency that isn't set up yet, set it up first before writing feature code. Don't hand-roll what the plan says to use a library for.
- **Auto-fix without asking:** bug fixes, type errors, missing imports, broken references, missing null checks, obvious error handling gaps
- **Auto-add without asking:** necessary validation, missing error handling on external calls, required interface implementations
- **Ask before doing:** new dependencies/packages, schema or data model changes, architectural decisions not covered in the plan, changes to public APIs or contracts
- **Never without explicit approval:** delete or restructure files not mentioned in the plan, change build/CI configuration, modify security boundaries

## Extra Rules

- Use the `idea` tag for ideas and proposals in our own nibs
- **IMPORTANT: If you encounter an error when using the `nibs` CLI or GraphQL, stop immediately. Evaluate what caused the error, determine the root cause, and suggest an action to mitigate it in the future (e.g., a code fix, a new nib to track the issue, or a workaround). Do not silently retry or ignore nibs errors.**
- No backwards compatibility requirements for CLI usage or APIs — this is a new, unreleased project. Only existing nibs data files (`.nibs/` Markdown+YAML format) must remain compatible. Feel free to make breaking CLI/API changes without migration shims.
- **Always prefer TDD when fixing bugs**: write a failing test that reproduces the bug first, then fix the code to make it pass.
- **Code review findings that are too large to fix in-place should be deferred as nibs**, not silently skipped. If a finding requires architectural redesign or broad refactoring beyond the current task's scope, create a follow-up nib to track it.
- **A finding is a claim, and a nib made from one inherits the claim without its evidence.** Defer findings with the command and output that demonstrate them, or label the claim `[Unverified]`. Reproduce a load-bearing premise before building on it: in one 7-nib batch, 4 premises were false — `disabled` blocks selection (it doesn't, in Chromium), a race detector fired 3/8 (80 runs say never), shorthand `#id` mentions don't link (they do), archiving removes a nib from the list (it doesn't). Every refutation came from running something; none from reading. The one premise that survived came from using the app.
- **When creating a new nib, place it at the appropriate position** using `reorderNib` (e.g. `afterId` of a related nib). Consider development dependencies, complexity, and type (bugs before refactors before features) when choosing where it belongs. Don't leave new nibs at the default position.

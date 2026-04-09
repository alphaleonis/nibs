# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is Nibs

Nibs is a file-based issue tracker for AI-first workflows. Issues ("nibs") are stored as Markdown files with YAML front matter in a `.nibs/` directory. It provides a CLI, a Bubbletea TUI, and a built-in GraphQL query engine that coding agents use to interact with nibs.

## Build & Dev Commands

All commands use [mise](https://mise.jdx.dev/) as the task runner:

- `mise build` - Build the `./nibs` executable (runs codegen first)
- `mise test` - Run all tests: Go + web (runs codegen and web:build first)
- `mise codegen` - Regenerate GraphQL code (`go generate ./...`)
- `mise nibs` - Build and run the CLI in one step (`go run .`)
- `go test ./internal/nib/` - Run tests for a specific package
- `cd web && npx vitest run --reporter=agent` - Run web tests only
- `mise demo` - Serve the web UI with the sample-project fixture (temporary copy, safe to mutate)
- `mise demo:tui` - Run the TUI with the sample-project fixture

## GraphQL Schema Changes

When modifying the GraphQL schema (`internal/graph/schema.graphqls`):

1. Edit the schema file
2. Run `mise codegen` to regenerate `internal/graph/generated.go` and `internal/graph/model/models_gen.go`
3. Implement any new resolvers in `internal/graph/schema.resolvers.go`

The code generation is configured in `gqlgen.yml`. The `nib.Nib` struct is autobound so the GraphQL `Nib` type maps directly to it.

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
- **`internal/config/`** - Configuration from `.nibs.yml`. Hardcoded enums: statuses (draft/todo/in-progress/completed/scrapped), types (milestone/epic/bug/feature/task), priorities (critical/high/normal/low/deferred)
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
- `web/dist/` is gitignored — run `mise build` (or `cd web && npm ci && npm run build`) to generate it before `go build`.
- Preferences (filter, view level, column widths, panel width) are persisted to localStorage via `web/src/lib/storage.ts`
- The table uses `table-layout: fixed` with an explicit computed width — column widths are enforced regardless of content

### Web UI Conventions

- **Event delegation**: TreeTable uses delegated event handlers on the scroll container (not per-row callbacks). TreeTableRow is a pure render component with zero callback props — interactive elements use `data-action` attributes (toggle, title, add-child, drag-handle). New actions require a handler case in TreeTable's `handleDelegatedClick`.
- **Svelte context for ambient state**: SelectionState and DragState are provided via `provideSelection`/`provideDrag` from `contexts.ts`. Components read with `useSelection()`/`useDrag()`. Tests must provide context via `makeTestContext()` from contexts.ts — pass as `context` option to `render()`.
- **Shared field components**: Use `StatusSelect`, `TypeSelect`, `PrioritySelect`, `EstimateSelect`, `TagEditor` from `web/src/lib/components/` instead of inline select/tag markup. Use `renderMarkdown()` from `web/src/lib/markdown.ts` instead of inline DOMPurify+marked. Use `.prose-nib` CSS class for markdown prose styling.

### Nib Data Model

Nibs are Markdown files with YAML front matter stored in `.nibs/`. Filename format: `{id}-{slug}.md` or `{id}.md`. Archived nibs go to `.nibs/archive/`. The `Nib` struct fields like `ID`, `Slug`, and `Path` are derived from the filename/path, not from front matter.

The `Path` field always uses forward slashes (normalized via `filepath.ToSlash`) for cross-platform portability. When using `Path` for filesystem operations, combine with `filepath.Join(c.root, b.Path)` which handles mixed separators.

### Configuration

Project config lives in `.nibs.yml` at project root (searched upward from cwd). Key settings: `nibs.prefix` (ID prefix like "myproj-"), `nibs.id_length`, `nibs.path` (data directory, default `.nibs`), `nibs.require_if_match` (optimistic concurrency). Nibs path can also be set via `--nibs-path` flag or `NIBS_PATH` env var.

For optional config fields with non-zero defaults, use pointer types (`*int`, `*bool`) with `yaml:"...,omitempty"` so nil means "use default" vs explicit zero/false. See `ServerConfig` for the pattern.

### Agent Integration

`nibs prime` outputs a prompt template (`cmd/prompt.tmpl`) that teaches coding agents how to use the GraphQL CLI. This is the primary interface for AI agents.

## Branching & Workflow

- The default branch is `main`
- We are not using pull requests currently — merge feature branches directly into `main` and push
- Create feature branches for non-trivial work, merge when done

## Commits

- Use conventional commit messages ("feat", "fix", "chore", etc.)
- Include relevant nib ID(s) in commit messages (e.g. `Refs: nib-xxxx`)
- Mark breaking changes with `!` notation (e.g. `feat!: ...`)
- Description should be a concise bullet point list of changes
- **Never commit with failing tests**, even if the failures appear unrelated to your changes. Run `mise test` before every commit — but if tests already passed since the last code change and no code has been modified since, skip the redundant run. If tests fail: either fix them in the same commit, or stash your changes (`git stash`), fix the tests and commit the fix separately, then reapply your stash and commit your work. Do not ignore or skip failing tests.
- **Never commit with build warnings.** Run `mise build` and check for warnings (deprecation notices, unused imports, etc.) before deeming a work item complete. Treat warnings as errors — fix them before committing.
- **Never commit with lint failures.** Run `mise lint` (golangci-lint) before committing. Fix all lint issues before committing.

## Testing

- Always write or update tests for changes
- Use table-driven tests following Go conventions
- Never hardcode `/` or `\` in path assertions — use `filepath.Join` for OS paths and forward slashes for nib `Path` fields
- For manual CLI testing: `mise nibs` compiles and runs the CLI
- For manual CLI testing, `mise demo` serves the web UI with a temporary copy of the sample-project fixture (safe to mutate), and `mise demo:tui` does the same for the TUI
- **Test fixture dataset**: `testdata/fixtures/sample-project/` has 87 curated nibs (prefix `tnib-`) covering all types, statuses, priorities, hierarchies, and relationships. Use `fixtures.CopySampleProject(t)` from `testdata/fixtures/` to get a temporary copy for write tests. Regenerate with `bash testdata/fixtures/gen-sample-project.sh`.
- Web UI tests: `cd web && npm install && npx vitest run --reporter=agent` (Vitest + jsdom + @testing-library/svelte). Run `npm install` first — node_modules can go stale after branch switches.
- Web test commands require `web/` as the working directory. If cwd has drifted, `cd` to the project root's `web/` directory first.
- **Always use `--reporter=agent`** when running vitest — it keeps output concise. Never pipe vitest through grep; read the output once.
- `mise test` runs both Go and web tests. No need to run them separately unless debugging a specific failure.
- **bits-ui timer flush**: `test-setup.ts` has an `afterAll` that waits 50ms so bits-ui's body-scroll-lock deferred cleanup (24ms setTimeout) fires while jsdom still exists. Without this, the timer fires after jsdom teardown causing a spurious "document is not defined" error.

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
- **When creating a new nib, place it at the appropriate position** using `reorderNib` (e.g. `afterId` of a related nib). Consider development dependencies, complexity, and type (bugs before refactors before features) when choosing where it belongs. Don't leave new nibs at the default position.

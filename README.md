# Nibs

A file-based issue tracker designed for AI-first workflows. Track tasks, bugs, and features as plain Markdown files right alongside your code — readable by humans, queryable by coding agents.

Originally forked from [hmans/beans](https://github.com/hmans/beans).

## Why Nibs?

Most issue trackers live outside your codebase. Your coding agent can't see them, can't search them, and can't update them. Nibs fixes this by storing issues as Markdown files in your repo, with a GraphQL query engine that gives agents exactly the context they need.

- **Your agent tracks its own work.** Nibs replaces in-context todo lists with persistent, structured issues that survive across sessions and compactions.
- **Everything is a file.** Issues are Markdown with YAML front matter in a `.nibs/` directory — version-controlled, diffable, and editable with any tool.
- **Hierarchy and relationships.** Milestones contain epics contain features contain tasks. Blocking/blocked-by relationships prevent your agent from starting work that depends on unfinished prerequisites.
- **Built-in GraphQL.** Agents query for exactly the fields they need, filter by status/type/priority, traverse relationships, and execute mutations — all from the CLI.

## Interfaces

Nibs provides three ways to interact with your issues:

- **CLI** (`nibs`) — Create, list, update, archive, and query nibs from the command line. All commands support `--json` for machine-readable output.
- **Web UI** (`nibs web`) — A Svelte-based web interface with a tree table, inline editing, drag-drop reordering, and a detail panel. Runs a local server.
- **TUI** (`nibs tui`) — A terminal UI built with Bubbletea for browsing and managing nibs without leaving the terminal.

## Installation

### Linux / macOS

```bash
curl -sSfL https://raw.githubusercontent.com/alphaleonis/nibs/main/install.sh | sh
```

Installs to `~/.local/bin` by default. Use `-b DIR` to change the install directory:

```bash
curl -sSfL https://raw.githubusercontent.com/alphaleonis/nibs/main/install.sh | sh -s -- -b /usr/local/bin
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/alphaleonis/nibs/main/install.ps1 | iex
```

Installs to `~/.local/bin` by default.

### From Source

Requires Node.js and [mise](https://mise.jdx.dev/) (mise installs Go, Task, and golangci-lint automatically from `mise.toml`):

```bash
git clone https://github.com/alphaleonis/nibs.git
cd nibs
mise install      # installs pinned Go, Task, golangci-lint
task build
```

Prebuilt binaries are also available on the [Releases](https://github.com/alphaleonis/nibs/releases) page.

### Updating

nibs notifies you when a newer release is available — a trailing line on the CLI, a footer hint in the TUI, and a banner in the web UI. To upgrade in place:

```bash
nibs upgrade            # download, verify, and replace the running binary (rolls back on failure)
nibs upgrade --check    # just report whether an update is available
```

If nibs was installed by a package manager (Homebrew, Nix, Scoop, Chocolatey, WinGet, or `go install`), `nibs upgrade` defers to that manager with guidance instead of replacing the binary itself. Set `NIBS_NO_UPDATE_CHECK=1` to silence the update notifications.

## Quick Start

Initialize nibs in your project:

```bash
nibs init
```

This creates a `.nibs/` directory and a `.nibs.yml` configuration file. Both should be tracked in version control.

```bash
nibs new "Set up CI pipeline" -t task    # create a task
nibs list                                # list all nibs
nibs tui                                 # interactive terminal UI
nibs web                                 # open the web UI in your browser
```

## Agent Integration

Nibs is designed to be used by coding agents like Claude Code, Cursor, Windsurf, and others. The `nibs prime` command outputs a slim prompt with mandatory workflow rules and a directive for the agent to load the full reference (`nibs prime --full`) before using any nibs commands. The slim default is what you want wired to a session-start hook; the full reference is fetched on demand.

### Claude Code

Add a `SessionStart` hook so the agent loads nibs context at the start of every conversation. Adding a `PreCompact` hook ensures the context is reloaded after the conversation history is compacted.

In `.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "nibs prime" }] }
    ],
    "PreCompact": [
      { "hooks": [{ "type": "command", "command": "nibs prime" }] }
    ]
  }
}
```

### Other Agents

Add the following instruction to your project's agent configuration file (e.g., `CLAUDE.md`, `.cursorrules`, `.windsurfrules`):

```
**IMPORTANT**: Run the `nibs prime` command and follow its output.
```

Or, if your agent framework supports startup hooks, wire `nibs prime` to run at session start.

## Key Commands

| Command | Description |
|---------|-------------|
| `nibs init` | Initialize a new nibs project |
| `nibs new` | Create a new nib (milestone, epic, feature, task, bug, or research) |
| `nibs list` | List nibs with filtering by status, type, priority, tags |
| `nibs get <id>` | Display one or more nibs (full document by default) |
| `nibs set <id>` | Update a nib's metadata and links, or clear a field |
| `nibs body <id>` | Edit a nib's Markdown body (set, append, or replace sections) |
| `nibs mv <id>` | Reposition a nib among its siblings or reparent it |
| `nibs close <id>` | Mark a nib completed with a summary |
| `nibs context` | Show project status summary with progress |
| `nibs plan <id>` | View an ordered plan of a parent nib's children |
| `nibs query` | Run a GraphQL query or mutation |
| `nibs roadmap` | Generate a Markdown roadmap from milestones and epics |
| `nibs check` | Validate configuration and data integrity |
| `nibs web` | Start the web UI server |
| `nibs tui` | Open the terminal UI |
| `nibs prime` | Output the agent integration prompt (slim default; pass `--full` for the complete reference) |
| `nibs archive` | Move completed/scrapped nibs to the archive |
| `nibs upgrade` | Update nibs to the latest release (checksum-verified, with rollback); `--check` to only check |

Run `nibs <command> --help` for full usage details.

## Data Model

Each nib has:

- **Type**: milestone, epic, feature, task, bug, or research
- **Status**: draft, todo, in-progress, deferred, completed, or scrapped
- **Priority** (optional): critical, high, normal, or low
- **Estimate** (optional): s, m, l, or xl (t-shirt sizes)
- **Tags**: freeform labels for categorization
- **Relationships**: parent/child hierarchy, blocking/blocked-by dependencies, document links

Nibs are stored as individual Markdown files in `.nibs/`, with YAML front matter for metadata and Markdown body for description and notes. Archived nibs move to `.nibs/archive/` and remain queryable.

## License

Licensed under the Apache-2.0 License. See [LICENSE.md](LICENSE.md) for details.

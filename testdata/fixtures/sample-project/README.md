# Sample Project Fixture

A curated test dataset of 89 nibs modeling a **TaskFlow** project management SaaS. Covers all nib types, statuses, priorities, estimates, tags, documents, areas, milestone assignments, parent/child hierarchies (4 levels deep), and blocking relationships.

## Contents

| Type      | Count | Statuses covered                                        |
|-----------|-------|---------------------------------------------------------|
| milestone | 2     | in-progress, draft                                      |
| epic      | 6     | in-progress, todo, draft                                |
| feature   | 20    | completed, in-progress, todo, draft, deferred, scrapped |
| task      | 44    | completed, in-progress, todo, draft, scrapped           |
| bug       | 15    | in-progress, todo, draft                                |
| research  | 2     | completed, todo                                         |

All 4 priorities (critical, high, normal, low), all 4 estimates (s, m, l, xl), 9 distinct tags, 4 document references, and 3 blocking relationships. The deferred status is exercised by the Slack integration feature.

## Areas

The store declares its own areas vocabulary in `.nibs/areas.yml` — the one vocabulary a project authors, where statuses, types, priorities and estimates are fixed. Two of the five roots nest a child, so a path is a real path and not just a name:

```
auth
api
api/webhooks
web
web/dashboard
infra
docs
```

Nibs are assigned across all of them **except `docs`, which is deliberately empty**. An area nothing is assigned to is a state the vocabulary exists to express, and the web's Areas view renders it as a row reading 0 — so `docs` is what a screenshot, a demo or an e2e run has to look at to see that. Do not tidy it away as unused, and do not assign work to it to make the fixture look complete.

## The 4 document references are deliberately broken

`docs/product-roadmap.md` (tnib-m001), `docs/auth-architecture.md` (tnib-e001) and `docs/websocket-rfc.md` (tnib-f014 and tnib-e005) name files this fixture does not ship, and that is on purpose: they are its only coverage of the broken-document-link finding. `nibs check` against a fresh copy is expected to report exactly those four and nothing else — no hierarchy findings, no other category.

Do not "fix" them by adding the files. `TestSampleProjectCheckFindingsArePinned` in `internal/nibcore` pins that set, so both adding a file and adding a fifth reference fail the suite.

## Usage

### CLI

```bash
# List all fixture nibs
nibs list --nibs-path testdata/fixtures/sample-project/.nibs

# Serve the web UI with fixture data
nibs serve --nibs-path testdata/fixtures/sample-project/.nibs
```

### In tests (Go)

```go
// Use a temporary copy so tests don't mutate the fixture
func copyFixture(t *testing.T) string {
    t.Helper()
    tmp := t.TempDir()
    // Copy testdata/fixtures/sample-project/.nibs/ to tmp/.nibs/ — the store
    // carries its own config and areas vocabulary, so nothing else needs copying
    return filepath.Join(tmp, ".nibs")
}
```

### In Playwright e2e tests

```bash
# Start server with a temporary copy of the fixture
cp -r testdata/fixtures/sample-project /tmp/test-project
nibs serve --nibs-path /tmp/test-project/.nibs --port 3001 &
# Run Playwright tests against localhost:3001
```

## Regenerating

```bash
bash testdata/fixtures/gen-sample-project.sh
```

This recreates all `.nibs/` files from scratch. The generator script is the source of truth — edit it to add or modify fixture nibs.

## Hierarchy

A bug's only legal parent is an epic, so every bug about a feature's subject matter hangs off that feature's epic instead.

The first edge below is not the same relation as the rest. A milestone ASSIGNS work rather than containing it, so the epics under each milestone carry a `milestone:` key and the milestone itself reports 0 children; everything from the epics down is genuine `parent:` nesting. The tree is drawn as one shape because it reads as one plan, not because the two edges are alike.

```
v1.0 MVP Launch (milestone)
├── User Authentication (epic)
│   ├── Email/password login (feature) ← 3 tasks
│   ├── OAuth2 social login (feature) ← 2 tasks + 1 research
│   ├── Password reset flow (feature) ← 2 tasks
│   ├── API documentation (task)
│   ├── Session token bug (bug)
│   └── OAuth callback URL bug (bug)
├── Task Management Core (epic)
│   ├── CRUD operations (feature) ← 3 tasks + 1 research
│   ├── Task assignment (feature) ← 2 tasks
│   ├── Prioritization (feature) ← 2 tasks
│   ├── Subtask support (feature) ← 2 tasks
│   ├── Performance benchmark (task)
│   ├── Title truncation bug (bug)
│   └── Deactivated-user assignment bug (bug)
├── Dashboard & Reporting (epic) [blocked by Task Management]
│   ├── Velocity chart (feature) ← 2 tasks
│   ├── Personal dashboard (feature) ← 2 tasks
│   ├── Export CSV/PDF (feature)
│   └── Timezone bug (bug)
└── API & Integrations (epic)
    ├── Webhook system (feature) ← 2 tasks
    ├── Slack integration (feature)
    ├── REST API v2 (feature, scrapped) ← 1 task
    └── Webhook timestamp bug (bug)

v1.1 Team Collaboration (milestone, draft)
├── Real-time Collaboration (epic)
│   ├── WebSocket live updates (feature) ← 2 tasks
│   └── Activity feed (feature) ← 1 task
└── Notifications System (epic)
    ├── In-app notifications (feature) ← 2 tasks
    ├── Email digest (feature) ← 1 task
    └── Push notification bug (bug)

Standalone: 11 tasks, 8 bugs, 3 features (various statuses)
```

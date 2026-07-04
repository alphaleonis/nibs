# Sample Project Fixture

A curated test dataset of 87 nibs modeling a **TaskFlow** project management SaaS. Covers all nib types, statuses, priorities, estimates, tags, documents, parent/child hierarchies (4 levels deep), and blocking relationships.

## Contents

| Type      | Count | Statuses covered                                      |
|-----------|-------|-------------------------------------------------------|
| milestone | 2     | in-progress, draft                                    |
| epic      | 6     | in-progress, todo, draft                              |
| feature   | 20    | completed, in-progress, todo, draft, deferred, scrapped |
| task      | 44    | completed, in-progress, todo, draft, scrapped         |
| bug       | 15    | in-progress, todo, draft                              |

All 4 priorities (critical, high, normal, low), all 4 estimates (s, m, l, xl), 9 distinct tags, 4 document references, and 3 blocking relationships. The deferred status is exercised by the Slack integration feature.

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
    // Copy testdata/fixtures/sample-project/.nibs/ to tmp/.nibs/
    // Copy testdata/fixtures/sample-project/.nibs.yml to tmp/
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

```
v1.0 MVP Launch (milestone)
├── User Authentication (epic)
│   ├── Email/password login (feature) ← 3 tasks
│   ├── OAuth2 social login (feature) ← 2 tasks + 1 bug
│   ├── Password reset flow (feature) ← 2 tasks
│   ├── Session token bug (bug)
│   └── API documentation (task)
├── Task Management Core (epic)
│   ├── CRUD operations (feature) ← 3 tasks
│   ├── Task assignment (feature) ← 2 tasks + 1 bug
│   ├── Prioritization (feature) ← 2 tasks
│   ├── Subtask support (feature) ← 2 tasks
│   ├── Title truncation bug (bug)
│   └── Performance benchmark (task)
├── Dashboard & Reporting (epic) [blocked by Task Management]
│   ├── Velocity chart (feature) ← 2 tasks
│   ├── Personal dashboard (feature) ← 2 tasks
│   ├── Export CSV/PDF (feature)
│   └── Timezone bug (bug)
└── API & Integrations (epic)
    ├── Webhook system (feature) ← 2 tasks + 1 bug
    ├── Slack integration (feature)
    └── REST API v2 (feature, scrapped) ← 1 task

v1.1 Team Collaboration (milestone, draft)
├── Real-time Collaboration (epic)
│   ├── WebSocket live updates (feature) ← 2 tasks
│   └── Activity feed (feature) ← 1 task
└── Notifications System (epic)
    ├── In-app notifications (feature) ← 2 tasks
    ├── Email digest (feature) ← 1 task
    └── Push notification bug (bug)

Standalone: 12 tasks, 8 bugs, 3 features (various statuses)
```

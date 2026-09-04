#!/bin/bash
# Generates the sample-project fixture dataset for UI and manual testing.
# Run from repo root: bash testdata/fixtures/gen-sample-project.sh
#
# The four `documents:` entries below name files this fixture DELIBERATELY does
# not ship — docs/product-roadmap.md (tnib-m001), docs/auth-architecture.md
# (tnib-e001) and docs/websocket-rfc.md (tnib-f014 and tnib-e005). They are the
# fixture's only coverage of the broken-document-link finding, so `nibs check`
# against it is expected to report exactly those four and nothing else. Do not
# "fix" them by adding the files; TestSampleProjectCheckFindingsArePinned in
# internal/nibcore pins that set, and adding a file fails it.
set -euo pipefail

STORE="testdata/fixtures/sample-project/.nibs"
DIR="$STORE/data"
rm -rf "$STORE"
mkdir -p "$DIR"

# The store holds its own config: prefix, id length and defaults travel with the
# data directory, so pointing nibs at this fixture applies the fixture's
# settings and not the surrounding project's.
cat > "$STORE/config.yml" << 'ENDCONFIG'
nibs:
    prefix: tnib-
    id_length: 4
    default_status: todo
    default_type: task
ENDCONFIG

# The areas vocabulary is its own file because it has its own lifetime: a
# running `nibs serve` reloads it when it changes, where everything in
# config.yml is fixed at startup. It declares every path the nibs below assign,
# plus `docs`, which nothing assigns: a declared area with no members is a state
# the vocabulary exists to express, and the web Areas view renders it as a row
# reading 0.
cat > "$STORE/areas.yml" << 'ENDAREAS'
areas:
    - name: auth
      description: Sign-in, sessions, tokens and account security
    - name: api
      description: The public HTTP API and the integrations built on it
      children:
        - name: webhooks
          description: Outbound webhook delivery and subscriptions
    - name: web
      description: The browser client
      children:
        - name: dashboard
          description: The project dashboard and its charts
    - name: infra
      description: Build, deployment and runtime infrastructure
    - name: docs
      description: Reference documentation and guides
ENDAREAS

# ============================================================
# MILESTONES (2)
# ============================================================

cat > "$DIR/tnib-m001--v1-mvp-launch.md" << 'ENDNIB'
---
# tnib-m001
version: 2
title: v1.0 MVP Launch
status: in-progress
type: milestone
priority: critical
created_at: 2026-02-15T09:00:00Z
updated_at: 2026-03-25T14:30:00Z
documents:
    - docs/product-roadmap.md
order: a
---

Ship the minimum viable product with core task management, user authentication, basic dashboard, and API foundation.

## Goal

Deliver a working product that early adopters can use for daily project management by end of Q1 2026.

## Current Focus

- Completing OAuth2 authentication flow
- Stabilizing task management CRUD operations
- Addressing critical security bugs before beta

## Key Decisions

- Chose PostgreSQL over MongoDB for ACID compliance requirements
- REST API first, GraphQL deferred to v1.2
- Server-side rendering deferred — SPA with Svelte for now
ENDNIB

cat > "$DIR/tnib-m002--v1-1-team-collaboration.md" << 'ENDNIB'
---
# tnib-m002
version: 2
title: v1.1 Team Collaboration
status: draft
type: milestone
priority: normal
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-20T11:00:00Z
order: b
---

Add real-time collaboration features, notification system, and activity feeds. Depends on v1.0 infrastructure being stable.

## Goal

Enable teams to collaborate in real-time on shared task boards with live updates and notifications.

## Key Decisions

- WebSocket for real-time (not SSE) — need bidirectional communication
- Event sourcing for activity feed to support retroactive queries
ENDNIB

# ============================================================
# EPICS (6)
# ============================================================

cat > "$DIR/tnib-e001--user-authentication.md" << 'ENDNIB'
---
# tnib-e001
version: 2
title: User Authentication
status: in-progress
type: epic
priority: high
created_at: 2026-02-18T10:00:00Z
updated_at: 2026-03-24T16:00:00Z
documents:
    - docs/auth-architecture.md
milestone: tnib-m001
milestone_order: a
---

## Objective

Implement a complete authentication system supporting email/password, OAuth2 social providers, and secure session management.

## Acceptance Criteria

- [x] Users can register and log in with email/password
- [x] Passwords hashed with bcrypt (cost 12)
- [ ] OAuth2 login via Google and GitHub
- [ ] Password reset via email link
- [ ] Sessions invalidated on password change

## Scope Boundaries

- MFA is out of scope for v1.0
- SAML/SSO deferred to enterprise tier
ENDNIB

cat > "$DIR/tnib-e002--task-management-core.md" << 'ENDNIB'
---
# tnib-e002
version: 2
title: Task Management Core
status: in-progress
type: epic
priority: high
created_at: 2026-02-18T10:30:00Z
updated_at: 2026-03-25T09:00:00Z
milestone: tnib-m001
milestone_order: b
---

## Objective

Build the core task CRUD, assignment, prioritization, and subtask systems that form the backbone of the product.

## Acceptance Criteria

- [x] Full CRUD for tasks with validation
- [x] Task assignment to users
- [ ] Priority-based sorting and drag-and-drop reordering
- [ ] Subtask/checklist support with depth limits

## Scope Boundaries

- Recurring tasks deferred to v1.2
- Gantt chart view deferred to v1.1
ENDNIB

cat > "$DIR/tnib-e003--dashboard-and-reporting.md" << 'ENDNIB'
---
# tnib-e003
version: 2
title: Dashboard & Reporting
status: todo
type: epic
priority: normal
created_at: 2026-02-20T11:00:00Z
updated_at: 2026-03-15T10:00:00Z
blocked_by:
    - tnib-e002
milestone: tnib-m001
milestone_order: c
---

## Objective

Provide team leads with velocity charts, personal dashboards, and exportable reports.

## Acceptance Criteria

- [ ] Team velocity chart with sprint-over-sprint comparison
- [ ] Personal "My Tasks" dashboard with widget layout
- [ ] Export to CSV and PDF

## Scope Boundaries

- Custom report builder deferred to v1.2
- Real-time dashboard updates require v1.1 WebSocket infrastructure
ENDNIB

cat > "$DIR/tnib-e004--api-and-integrations.md" << 'ENDNIB'
---
# tnib-e004
version: 2
title: API & Integrations
status: todo
type: epic
priority: normal
created_at: 2026-02-20T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
milestone: tnib-m001
milestone_order: d
---

## Objective

Expose a webhook system for third-party integrations and lay groundwork for future Slack/GitHub integrations.

## Acceptance Criteria

- [ ] Webhook registration and delivery with retry
- [ ] At least one external integration (Slack or GitHub)
- [ ] REST API documented with OpenAPI spec

## Scope Boundaries

- GraphQL API deferred to v1.2
- Two-way sync with external tools deferred
ENDNIB

cat > "$DIR/tnib-e005--real-time-collaboration.md" << 'ENDNIB'
---
# tnib-e005
version: 2
title: Real-time Collaboration
status: draft
type: epic
priority: normal
estimate: xl
created_at: 2026-03-10T10:00:00Z
updated_at: 2026-03-18T09:00:00Z
documents:
    - docs/websocket-rfc.md
milestone: tnib-m002
milestone_order: a
---

## Objective

Enable live multi-user editing with WebSocket-based updates and conflict resolution.

## Acceptance Criteria

- [ ] WebSocket server with connection management
- [ ] Optimistic UI updates with server reconciliation
- [ ] Activity feed powered by event sourcing

## Scope Boundaries

- Operational transform / CRDT for rich text editing deferred
- Voice/video collaboration out of scope
ENDNIB

cat > "$DIR/tnib-e006--notifications-system.md" << 'ENDNIB'
---
# tnib-e006
version: 2
title: Notifications System
status: draft
type: epic
priority: normal
created_at: 2026-03-10T11:00:00Z
updated_at: 2026-03-18T09:00:00Z
milestone: tnib-m002
milestone_order: b
---

## Objective

Deliver in-app and email notifications for task assignments, mentions, and deadline reminders.

## Acceptance Criteria

- [ ] In-app notification center with read/unread state
- [ ] Email digest (daily/weekly configurable)
- [ ] Push notifications on mobile (PWA)

## Scope Boundaries

- SMS notifications out of scope
- Notification preferences UI in v1.1, API-only in v1.0
ENDNIB

# ============================================================
# FEATURES under User Authentication (tnib-e001)
# ============================================================

cat > "$DIR/tnib-f001--email-password-login.md" << 'ENDNIB'
---
# tnib-f001
version: 2
title: Email/password login
status: completed
type: feature
priority: high
estimate: m
created_at: 2026-02-20T09:00:00Z
updated_at: 2026-03-05T17:00:00Z
parent: tnib-e001
order: a
---

Standard email/password authentication with bcrypt hashing, rate limiting, and session cookies.

## Acceptance Criteria

- [x] Registration with email validation
- [x] Login with bcrypt password verification
- [x] Rate limiting (5 attempts per 15 minutes)
- [x] Secure HTTP-only session cookies
ENDNIB

cat > "$DIR/tnib-f002--oauth2-social-login.md" << 'ENDNIB'
---
# tnib-f002
version: 2
title: OAuth2 social login
status: in-progress
type: feature
priority: normal
estimate: l
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-24T15:00:00Z
parent: tnib-e001
order: b
area: auth
---

Allow users to log in via Google and GitHub OAuth2 providers. Account linking when email matches existing account.

## Acceptance Criteria

- [x] Google OAuth2 login flow
- [ ] GitHub OAuth2 login flow
- [ ] Account linking for matching emails
- [ ] Graceful handling of denied permissions
ENDNIB

cat > "$DIR/tnib-f003--password-reset-flow.md" << 'ENDNIB'
---
# tnib-f003
version: 2
title: Password reset flow
status: todo
type: feature
priority: normal
estimate: m
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e001
order: c
---

Email-based password reset with secure token generation and configurable expiry.

## Acceptance Criteria

- [ ] "Forgot password" page sends reset email
- [ ] Token valid for 1 hour, single-use
- [ ] Rate limiting on reset requests
- [ ] Password strength validation on new password
ENDNIB

# ============================================================
# FEATURES under Task Management Core (tnib-e002)
# ============================================================

cat > "$DIR/tnib-f004--crud-operations-for-tasks.md" << 'ENDNIB'
---
# tnib-f004
version: 2
title: CRUD operations for tasks
status: completed
type: feature
priority: high
estimate: l
created_at: 2026-02-20T10:00:00Z
updated_at: 2026-03-08T16:00:00Z
parent: tnib-e002
order: a
---

Full create, read, update, delete operations for tasks with input validation and audit logging.

## Acceptance Criteria

- [x] Create task with title, description, priority, assignee
- [x] Read task by ID, list with pagination and filtering
- [x] Update any task field with optimistic concurrency
- [x] Soft delete with 30-day retention
- [x] Audit log entries for all mutations
ENDNIB

cat > "$DIR/tnib-f005--task-assignment-and-ownership.md" << 'ENDNIB'
---
# tnib-f005
version: 2
title: Task assignment and ownership
status: in-progress
type: feature
priority: normal
estimate: m
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-23T11:00:00Z
parent: tnib-e002
order: b
---

Assign tasks to team members with ownership tracking and notification on assignment changes.

## Acceptance Criteria

- [x] Assign/unassign users to tasks
- [x] User-task relationship model
- [ ] Email notification on assignment
- [ ] Bulk assignment via API
ENDNIB

cat > "$DIR/tnib-f006--task-prioritization-and-sorting.md" << 'ENDNIB'
---
# tnib-f006
version: 2
title: Task prioritization and sorting
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-05T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e002
order: c
---

Allow users to prioritize tasks with drag-and-drop reordering and optional auto-sorting by priority level.

## Acceptance Criteria

- [ ] Drag-and-drop reordering within a list
- [ ] Priority-based auto-sort toggle
- [ ] Persisted custom order per user per board
ENDNIB

cat > "$DIR/tnib-f007--subtask-and-checklist-support.md" << 'ENDNIB'
---
# tnib-f007
version: 2
title: Subtask and checklist support
status: draft
type: feature
priority: low
estimate: xl
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e002
order: d
---

Support nested subtasks and checklists within tasks, with progress aggregation and configurable depth limits.

## Acceptance Criteria

- [ ] Create subtasks nested under a parent task
- [ ] Checklist items with completion toggle
- [ ] Progress percentage aggregated from subtask completion
- [ ] Configurable max nesting depth (default: 3)
ENDNIB

# ============================================================
# FEATURES under Dashboard & Reporting (tnib-e003)
# ============================================================

cat > "$DIR/tnib-f008--team-velocity-chart.md" << 'ENDNIB'
---
# tnib-f008
version: 2
title: Team velocity chart
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-05T11:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e003
order: a
area: web/dashboard
---

Sprint-over-sprint velocity chart showing completed story points per sprint with trend line.

## Acceptance Criteria

- [ ] Bar chart showing points completed per sprint
- [ ] Rolling average trend line
- [ ] Configurable sprint length (1-4 weeks)
- [ ] Export chart as PNG
ENDNIB

cat > "$DIR/tnib-f009--personal-task-dashboard.md" << 'ENDNIB'
---
# tnib-f009
version: 2
title: Personal task dashboard
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-05T11:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e003
order: b
---

Customizable personal dashboard showing assigned tasks, upcoming deadlines, and activity summary.

## Acceptance Criteria

- [ ] "My Tasks" widget with filtering
- [ ] Upcoming deadlines widget (next 7 days)
- [ ] Recent activity feed widget
- [ ] Draggable widget layout (persisted per user)
ENDNIB

cat > "$DIR/tnib-f010--export-reports-csv-pdf.md" << 'ENDNIB'
---
# tnib-f010
version: 2
title: Export reports to CSV and PDF
status: draft
type: feature
priority: low
estimate: l
created_at: 2026-03-08T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e003
order: c
---

Allow exporting task lists, sprint reports, and velocity data as CSV or PDF files.

## Acceptance Criteria

- [ ] CSV export for task lists with all fields
- [ ] PDF report with charts and summary
- [ ] Scheduled email reports (weekly digest)
ENDNIB

# ============================================================
# FEATURES under API & Integrations (tnib-e004)
# ============================================================

cat > "$DIR/tnib-f011--webhook-delivery-system.md" << 'ENDNIB'
---
# tnib-f011
version: 2
title: Webhook delivery system
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-05T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e004
order: a
area: api/webhooks
---

HTTP webhook system for notifying external services of task events (created, updated, deleted, assigned).

## Acceptance Criteria

- [ ] Register webhook URLs with event type filters
- [ ] HMAC-SHA256 signature verification
- [ ] Retry with exponential backoff (max 5 attempts)
- [ ] Delivery log with status and response time
ENDNIB

cat > "$DIR/tnib-f012--slack-integration.md" << 'ENDNIB'
---
# tnib-f012
version: 2
title: Slack integration
status: deferred
type: feature
priority: low
estimate: l
created_at: 2026-03-08T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e004
order: b
---

Slack bot that posts task updates to channels and allows creating tasks via slash commands.

## Acceptance Criteria

- [ ] Slack app with OAuth2 installation flow
- [ ] `/taskflow create` slash command
- [ ] Channel notifications for task events
- [ ] Thread replies linked to task comments
ENDNIB

cat > "$DIR/tnib-f013--rest-api-v2-openapi.md" << 'ENDNIB'
---
# tnib-f013
version: 2
title: REST API v2 with OpenAPI
status: scrapped
type: feature
priority: normal
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-10T15:00:00Z
parent: tnib-e004
order: c
---

Redesigned REST API with auto-generated OpenAPI documentation and breaking changes from v1.

Scrapped in favor of incremental v1 improvements. A full v2 rewrite would delay the MVP without enough user-facing benefit.
ENDNIB

# ============================================================
# FEATURES under Real-time Collaboration (tnib-e005)
# ============================================================

cat > "$DIR/tnib-f014--websocket-live-updates.md" << 'ENDNIB'
---
# tnib-f014
version: 2
title: WebSocket live updates
status: draft
type: feature
priority: normal
estimate: xl
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e005
documents:
    - docs/websocket-rfc.md
order: a
---

Real-time task board updates via WebSocket connections with optimistic UI and server reconciliation.

## Acceptance Criteria

- [ ] WebSocket server with connection lifecycle management
- [ ] Client-side optimistic updates with rollback on conflict
- [ ] Presence indicators (who's viewing what)
- [ ] Graceful reconnection with state sync
ENDNIB

cat > "$DIR/tnib-f015--activity-feed.md" << 'ENDNIB'
---
# tnib-f015
version: 2
title: Activity feed
status: draft
type: feature
priority: normal
estimate: l
created_at: 2026-03-12T10:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e005
order: b
---

Chronological activity feed showing task changes, comments, and team activity powered by event sourcing.

## Acceptance Criteria

- [ ] Event sourcing pipeline capturing all task mutations
- [ ] Feed UI with infinite scroll
- [ ] Filter by user, project, or event type
- [ ] @mention highlighting
ENDNIB

# ============================================================
# FEATURES under Notifications System (tnib-e006)
# ============================================================

cat > "$DIR/tnib-f016--in-app-notification-center.md" << 'ENDNIB'
---
# tnib-f016
version: 2
title: In-app notification center
status: draft
type: feature
priority: normal
estimate: m
created_at: 2026-03-12T11:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e006
order: a
---

Bell icon notification center with real-time badge count and read/unread state management.

## Acceptance Criteria

- [ ] Notification bell with unread count badge
- [ ] Dropdown panel with recent notifications
- [ ] Mark as read (individual and bulk)
- [ ] Click-through to relevant task/comment
ENDNIB

cat > "$DIR/tnib-f017--email-digest-notifications.md" << 'ENDNIB'
---
# tnib-f017
version: 2
title: Email digest notifications
status: draft
type: feature
priority: low
estimate: m
created_at: 2026-03-12T11:30:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e006
order: b
---

Configurable email digest summarizing task updates, mentions, and upcoming deadlines.

## Acceptance Criteria

- [ ] Daily or weekly digest (user-configurable)
- [ ] Summary of assigned task changes
- [ ] Upcoming deadline reminders
- [ ] Unsubscribe link in every email
ENDNIB

# ============================================================
# STANDALONE FEATURES (no parent)
# ============================================================

cat > "$DIR/tnib-f018--dark-mode-theme.md" << 'ENDNIB'
---
# tnib-f018
version: 2
title: Dark mode theme support
status: scrapped
type: feature
priority: low
tags:
    - ux
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-12T15:00:00Z
order: r
---

System-preference-aware dark mode theme. Scrapped — premature for MVP. Will revisit after v1.0 based on user feedback.
ENDNIB

cat > "$DIR/tnib-f019--keyboard-shortcuts.md" << 'ENDNIB'
---
# tnib-f019
version: 2
title: Keyboard shortcuts for power users
status: todo
type: feature
priority: normal
estimate: m
tags:
    - ux
created_at: 2026-03-15T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: s
---

Vim-inspired keyboard shortcuts for task navigation, creation, and status changes.

## Acceptance Criteria

- [ ] `j`/`k` for list navigation
- [ ] `c` to create task, `e` to edit
- [ ] `1`-`5` to set priority
- [ ] `?` to show shortcut cheat sheet
ENDNIB

cat > "$DIR/tnib-f020--bulk-task-operations.md" << 'ENDNIB'
---
# tnib-f020
version: 2
title: Bulk task operations
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-15T10:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: t
---

Select multiple tasks and perform bulk operations (assign, change status, move, delete).

## Acceptance Criteria

- [ ] Checkbox-based multi-select in list view
- [ ] Bulk status change
- [ ] Bulk assignment
- [ ] Batch API endpoint for programmatic use
ENDNIB

# ============================================================
# TASKS under User Authentication features
# ============================================================

cat > "$DIR/tnib-t001--implement-bcrypt-hashing.md" << 'ENDNIB'
---
# tnib-t001
version: 2
title: Implement bcrypt password hashing
status: completed
type: task
priority: high
estimate: s
created_at: 2026-02-20T10:00:00Z
updated_at: 2026-02-25T16:00:00Z
parent: tnib-f001
order: a
---

## Description

Implement bcrypt hashing with cost factor 12 for password storage. Use `golang.org/x/crypto/bcrypt` package.

## Verification

- [x] Passwords stored as bcrypt hashes, never plaintext
- [x] Cost factor configurable via environment variable
- [x] Timing-safe comparison for login verification
ENDNIB

cat > "$DIR/tnib-t002--rate-limiting-login.md" << 'ENDNIB'
---
# tnib-t002
version: 2
title: Add rate limiting to login endpoint
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-01T14:00:00Z
parent: tnib-f001
order: b
---

## Description

Rate limit login attempts to 5 per 15 minutes per IP using Redis-backed sliding window counter.

## Verification

- [x] 429 response after 5 failed attempts
- [x] Counter resets after 15 minutes
- [x] Separate counters per IP address
- [x] X-RateLimit-* headers in response
ENDNIB

cat > "$DIR/tnib-t003--login-integration-tests.md" << 'ENDNIB'
---
# tnib-t003
version: 2
title: Write login integration tests
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-03T11:00:00Z
parent: tnib-f001
order: c
---

## Description

Integration tests for the full login flow including registration, login, session management, and rate limiting.

## Verification

- [x] Happy path: register → login → authenticated request
- [x] Wrong password returns 401
- [x] Rate limiting kicks in after threshold
- [x] Session cookie is HTTP-only and Secure
ENDNIB

cat > "$DIR/tnib-t004--google-oauth-integration.md" << 'ENDNIB'
---
# tnib-t004
version: 2
title: Google OAuth provider integration
status: completed
type: task
priority: normal
estimate: m
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-15T16:00:00Z
parent: tnib-f002
order: a
---

## Description

Implement Google OAuth2 login using the authorization code flow. Extract email and profile from ID token.

## Verification

- [x] Redirect to Google consent screen
- [x] Handle callback and exchange code for tokens
- [x] Create or link user account from Google profile
- [x] Handle denied permissions gracefully
ENDNIB

cat > "$DIR/tnib-t005--github-oauth-integration.md" << 'ENDNIB'
---
# tnib-t005
version: 2
title: GitHub OAuth provider integration
status: in-progress
type: task
priority: normal
estimate: m
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-24T14:00:00Z
parent: tnib-f002
order: b
---

## Description

Implement GitHub OAuth2 login. Similar to Google flow but uses GitHub's user API for profile data instead of ID token.

## Verification

- [x] Redirect to GitHub authorization page
- [x] Handle callback and token exchange
- [ ] Extract email from GitHub user API (may require email scope)
- [ ] Account linking when GitHub email matches existing user
ENDNIB

cat > "$DIR/tnib-t006--password-reset-email-template.md" << 'ENDNIB'
---
# tnib-t006
version: 2
title: Design password reset email template
status: todo
type: task
priority: normal
estimate: s
created_at: 2026-03-05T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f003
order: a
---

## Description

Create responsive HTML email template for password reset. Must work in major email clients (Gmail, Outlook, Apple Mail).

## Verification

- [ ] Responsive HTML template with inline CSS
- [ ] Includes reset link, expiry notice, and security warning
- [ ] Tested in Litmus for major email clients
- [ ] Plain text fallback included
ENDNIB

cat > "$DIR/tnib-t007--token-generation-expiry.md" << 'ENDNIB'
---
# tnib-t007
version: 2
title: Implement token generation and expiry
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-05T09:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f003
order: b
---

## Description

Generate cryptographically secure reset tokens with 1-hour TTL. Tokens are single-use and stored hashed in the database.

## Verification

- [ ] Tokens generated with crypto/rand (32 bytes, base64url encoded)
- [ ] Stored as SHA-256 hash in database
- [ ] Expired tokens rejected with clear error message
- [ ] Used tokens cannot be reused
ENDNIB

cat > "$DIR/tnib-t008--auth-api-documentation.md" << 'ENDNIB'
---
# tnib-t008
version: 2
title: Write authentication API documentation
status: draft
type: task
priority: low
estimate: m
tags:
    - docs
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e001
order: d
---

## Description

Document all authentication endpoints in the API reference: registration, login, logout, OAuth flows, password reset, and session management.

## Verification

- [ ] OpenAPI spec updated with auth endpoints
- [ ] Request/response examples for each endpoint
- [ ] Error code reference table
- [ ] Authentication flow diagrams
ENDNIB

# ============================================================
# TASKS under Task Management features
# ============================================================

cat > "$DIR/tnib-t009--database-schema-design.md" << 'ENDNIB'
---
# tnib-t009
version: 2
title: Design database schema for tasks
status: completed
type: task
priority: high
estimate: m
created_at: 2026-02-20T11:00:00Z
updated_at: 2026-03-01T10:00:00Z
parent: tnib-f004
order: a
---

## Description

Design PostgreSQL schema for tasks table with proper indexes, constraints, and migration scripts.

## Verification

- [x] Tasks table with all required columns
- [x] Indexes on commonly queried fields (status, assignee, priority)
- [x] Foreign key constraints for user references
- [x] Migration script (up and down)
ENDNIB

cat > "$DIR/tnib-t010--rest-api-endpoints.md" << 'ENDNIB'
---
# tnib-t010
version: 2
title: Implement REST API endpoints
status: completed
type: task
priority: high
estimate: m
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-05T16:00:00Z
parent: tnib-f004
order: b
---

## Description

Implement CRUD REST endpoints: POST /tasks, GET /tasks, GET /tasks/:id, PATCH /tasks/:id, DELETE /tasks/:id.

## Verification

- [x] All five endpoints implemented with proper HTTP methods
- [x] Pagination on list endpoint (cursor-based)
- [x] Filtering by status, assignee, priority
- [x] Proper error responses (400, 404, 409, 500)
ENDNIB

cat > "$DIR/tnib-t011--input-validation.md" << 'ENDNIB'
---
# tnib-t011
version: 2
title: Add input validation and sanitization
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-06T14:00:00Z
parent: tnib-f004
order: c
---

## Description

Validate all task input fields: title length, description size, valid enum values for status/priority, XSS sanitization.

## Verification

- [x] Title: 1-500 chars, trimmed
- [x] Description: max 50KB, HTML sanitized
- [x] Status and priority: validated against allowed values
- [x] Error messages include field name and constraint
ENDNIB

cat > "$DIR/tnib-t012--user-task-relationship.md" << 'ENDNIB'
---
# tnib-t012
version: 2
title: Create user-task relationship model
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-03-01T10:00:00Z
updated_at: 2026-03-10T15:00:00Z
parent: tnib-f005
order: a
---

## Description

Create the many-to-many relationship model between users and tasks for assignments. Support multiple assignees per task.

## Verification

- [x] Join table with user_id, task_id, role (assignee/reviewer)
- [x] Unique constraint on (user_id, task_id, role)
- [x] Cascade delete when task is deleted
ENDNIB

cat > "$DIR/tnib-t013--assignment-notification-emails.md" << 'ENDNIB'
---
# tnib-t013
version: 2
title: Build assignment notification emails
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-05T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f005
blocked_by:
    - tnib-e006
order: b
---

## Description

Send email notifications when a task is assigned to a user. Depends on the notifications system infrastructure.

## Verification

- [ ] Email sent on task assignment
- [ ] Email includes task title, description, and link
- [ ] Respects user notification preferences
- [ ] Batch multiple assignments into single email
ENDNIB

cat > "$DIR/tnib-t014--drag-and-drop-reordering.md" << 'ENDNIB'
---
# tnib-t014
version: 2
title: Implement drag-and-drop reordering
status: todo
type: task
priority: normal
estimate: l
created_at: 2026-03-08T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f006
order: a
---

## Description

Drag-and-drop task reordering using fractional indexing for order persistence. Must work on both desktop and touch devices.

## Verification

- [ ] Drag handle on each task row
- [ ] Visual feedback during drag (ghost element, drop indicators)
- [ ] Fractional index updates (no full reindex)
- [ ] Touch device support with long-press activation
ENDNIB

cat > "$DIR/tnib-t015--priority-auto-sorting.md" << 'ENDNIB'
---
# tnib-t015
version: 2
title: Add priority-based auto-sorting
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T09:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f006
order: b
---

## Description

Toggle to auto-sort tasks by priority (critical → high → normal → low → deferred). Manual order preserved as fallback.

## Verification

- [ ] Toggle button in list header
- [ ] Sort is stable (preserves relative order within same priority)
- [ ] Custom order restored when auto-sort is disabled
ENDNIB

cat > "$DIR/tnib-t016--recursive-depth-limit.md" << 'ENDNIB'
---
# tnib-t016
version: 2
title: Enforce recursive depth limit
status: draft
type: task
priority: normal
estimate: m
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f007
order: a
---

## Description

Prevent infinite nesting by enforcing a configurable maximum subtask depth (default: 3 levels).

## Verification

- [ ] API rejects subtask creation beyond max depth
- [ ] Clear error message with current depth
- [ ] Max depth configurable per project (admin setting)
ENDNIB

cat > "$DIR/tnib-t017--subtask-progress-aggregation.md" << 'ENDNIB'
---
# tnib-t017
version: 2
title: Subtask progress aggregation
status: draft
type: task
priority: normal
estimate: m
created_at: 2026-03-12T09:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f007
order: b
---

## Description

Calculate and display parent task progress as percentage of completed subtasks. Recursive aggregation for nested subtasks.

## Verification

- [ ] Progress bar on parent tasks showing subtask completion
- [ ] Recursive calculation (grandchild tasks included)
- [ ] Progress updates in real-time on completion toggle
ENDNIB

cat > "$DIR/tnib-t018--performance-benchmark-10k.md" << 'ENDNIB'
---
# tnib-t018
version: 2
title: Performance benchmark for 10k tasks
status: todo
type: task
priority: normal
estimate: m
tags:
    - perf
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e002
order: e
---

## Description

Load test with 10,000 tasks to establish baseline performance metrics. Identify bottlenecks in list rendering and API response times.

## Verification

- [ ] Seed script creates 10k tasks with realistic data
- [ ] API list endpoint responds in <200ms at p95
- [ ] UI list renders without jank (60fps scroll)
- [ ] Document baseline metrics for future regression checks
ENDNIB

# ============================================================
# TASKS under Dashboard & Reporting features
# ============================================================

cat > "$DIR/tnib-t019--charting-library-integration.md" << 'ENDNIB'
---
# tnib-t019
version: 2
title: Integrate charting library
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T11:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f008
order: a
---

## Description

Evaluate and integrate a charting library (Chart.js or D3) for velocity and burndown charts.

## Verification

- [ ] Library chosen and integrated (bundle size impact documented)
- [ ] Basic bar chart rendering with sample data
- [ ] Responsive sizing for different viewports
ENDNIB

cat > "$DIR/tnib-t020--sprint-data-aggregation.md" << 'ENDNIB'
---
# tnib-t020
version: 2
title: Build sprint data aggregation query
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T11:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f008
order: b
---

## Description

SQL query and Go service method to aggregate completed story points per sprint, with rolling average calculation.

## Verification

- [ ] Query returns points completed per sprint
- [ ] Handles sprints with zero completions
- [ ] Rolling average over configurable window (default: 5 sprints)
ENDNIB

cat > "$DIR/tnib-t021--widget-layout-system.md" << 'ENDNIB'
---
# tnib-t021
version: 2
title: Design widget layout system
status: todo
type: task
priority: normal
estimate: l
created_at: 2026-03-08T12:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f009
order: a
---

## Description

CSS Grid-based dashboard layout system with draggable, resizable widgets. Layout persisted per user in local storage.

## Verification

- [ ] Grid layout with configurable columns (2-4)
- [ ] Widgets draggable to reorder
- [ ] Widget resize within grid constraints
- [ ] Layout saved to and restored from local storage
ENDNIB

cat > "$DIR/tnib-t022--my-tasks-widget.md" << 'ENDNIB'
---
# tnib-t022
version: 2
title: Build "my tasks" summary widget
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T12:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f009
order: b
---

## Description

Dashboard widget showing the current user's assigned tasks grouped by status with counts and quick-action buttons.

## Verification

- [ ] Tasks grouped by status (to-do, in-progress, done)
- [ ] Count badge per group
- [ ] Click to navigate to task
- [ ] Quick status change buttons
ENDNIB

# ============================================================
# TASKS under API & Integrations features
# ============================================================

cat > "$DIR/tnib-t023--webhook-registration-endpoint.md" << 'ENDNIB'
---
# tnib-t023
version: 2
title: Build webhook registration endpoint
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f011
order: a
---

## Description

REST endpoint to register, list, update, and delete webhook subscriptions. Each webhook specifies a URL and event types.

## Verification

- [ ] POST /webhooks to register
- [ ] GET /webhooks to list
- [ ] DELETE /webhooks/:id to remove
- [ ] Validation: URL must be HTTPS, event types must be valid
ENDNIB

cat > "$DIR/tnib-t024--webhook-retry-backoff.md" << 'ENDNIB'
---
# tnib-t024
version: 2
title: Implement retry with exponential backoff
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T14:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f011
order: b
---

## Description

Background worker that retries failed webhook deliveries with exponential backoff: 1m, 5m, 30m, 2h, 12h.

## Verification

- [ ] Failed deliveries queued for retry
- [ ] Backoff schedule: 1m → 5m → 30m → 2h → 12h
- [ ] Max 5 attempts before marking as permanently failed
- [ ] Webhook disabled after 10 consecutive failures
ENDNIB

cat > "$DIR/tnib-t025--openapi-spec-generation.md" << 'ENDNIB'
---
# tnib-t025
version: 2
title: Auto-generate OpenAPI spec from code
status: scrapped
type: task
priority: normal
created_at: 2026-02-28T09:00:00Z
updated_at: 2026-03-10T15:00:00Z
parent: tnib-f013
order: a
---

## Description

Set up swag or similar tool to auto-generate OpenAPI spec from Go code annotations.

Scrapped along with REST API v2 — maintaining generated OpenAPI in sync with rapidly changing v1 code is more overhead than benefit at this stage.
ENDNIB

# ============================================================
# TASKS under Real-time Collaboration features
# ============================================================

cat > "$DIR/tnib-t026--websocket-server-setup.md" << 'ENDNIB'
---
# tnib-t026
version: 2
title: Set up WebSocket server infrastructure
status: draft
type: task
priority: normal
estimate: l
created_at: 2026-03-14T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f014
order: a
---

## Description

WebSocket server using gorilla/websocket with connection management, heartbeat/ping, and room-based pub/sub.

## Verification

- [ ] WebSocket endpoint at /ws with JWT authentication
- [ ] Connection pool with room-based subscriptions
- [ ] Heartbeat ping every 30s, disconnect after 90s silence
- [ ] Graceful shutdown with client notification
ENDNIB

cat > "$DIR/tnib-t027--conflict-resolution-strategy.md" << 'ENDNIB'
---
# tnib-t027
version: 2
title: Design conflict resolution strategy
status: draft
type: task
priority: normal
estimate: l
created_at: 2026-03-14T09:30:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f014
order: b
---

## Description

Define how concurrent edits are resolved: last-write-wins for simple fields, operational transform or CRDT for text fields.

## Verification

- [ ] Strategy document with rationale
- [ ] Last-write-wins for status, priority, assignee changes
- [ ] Conflict detection with user notification for concurrent edits
- [ ] Prototype demonstrating conflict resolution
ENDNIB

cat > "$DIR/tnib-t028--event-sourcing-pipeline.md" << 'ENDNIB'
---
# tnib-t028
version: 2
title: Build event sourcing pipeline
status: draft
type: task
priority: normal
estimate: xl
created_at: 2026-03-14T10:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f015
order: a
---

## Description

Event sourcing system that captures all task mutations as immutable events. Powers the activity feed and enables audit trail.

## Verification

- [ ] All task mutations produce events (created, updated, deleted, assigned, etc.)
- [ ] Events stored in append-only events table
- [ ] Event replay can reconstruct current state
- [ ] Event stream consumable by WebSocket for live feed
ENDNIB

# ============================================================
# TASKS under Notifications System features
# ============================================================

cat > "$DIR/tnib-t029--notification-center-ui.md" << 'ENDNIB'
---
# tnib-t029
version: 2
title: Design notification center UI
status: draft
type: task
priority: normal
estimate: m
created_at: 2026-03-14T11:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f016
order: a
---

## Description

UI component for the notification center: bell icon with badge, dropdown panel with notification list, and empty state.

## Verification

- [ ] Bell icon with animated badge on new notifications
- [ ] Dropdown panel with scrollable notification list
- [ ] Notification types: assignment, mention, deadline, comment
- [ ] Empty state illustration and message
ENDNIB

cat > "$DIR/tnib-t030--read-unread-state.md" << 'ENDNIB'
---
# tnib-t030
version: 2
title: Implement read/unread state management
status: draft
type: task
priority: normal
estimate: s
created_at: 2026-03-14T11:30:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f016
order: b
---

## Description

Track read/unread state per user per notification with "mark all as read" bulk action.

## Verification

- [ ] Unread notifications visually distinct (bold, dot indicator)
- [ ] Click marks individual notification as read
- [ ] "Mark all as read" button in notification panel
- [ ] Unread count in bell badge updates in real-time
ENDNIB

cat > "$DIR/tnib-t031--email-template-system.md" << 'ENDNIB'
---
# tnib-t031
version: 2
title: Build email template system
status: draft
type: task
priority: normal
estimate: m
created_at: 2026-03-14T12:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f017
order: a
---

## Description

Templated email system using Go html/template with a base layout, reusable components, and per-notification-type templates.

## Verification

- [ ] Base email layout with header, content area, footer
- [ ] Templates for: assignment, mention, deadline, digest
- [ ] Preview endpoint for development (renders template with sample data)
- [ ] Plain text and HTML versions for each template
ENDNIB

# ============================================================
# TASKS under Bulk Operations feature (tnib-f020)
# ============================================================

cat > "$DIR/tnib-t041--bulk-selection-ui.md" << 'ENDNIB'
---
# tnib-t041
version: 2
title: Design bulk selection UI
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-18T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
parent: tnib-f020
order: a
---

## Description

Add checkbox column to task list with shift-click range select and "select all" toggle. Show floating action bar when items are selected.

## Verification

- [ ] Checkbox column in list view
- [ ] Shift-click for range selection
- [ ] "Select all" checkbox in header
- [ ] Floating action bar appears on selection with available actions
ENDNIB

cat > "$DIR/tnib-t042--batch-api-endpoint.md" << 'ENDNIB'
---
# tnib-t042
version: 2
title: Implement batch API endpoint
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-18T09:30:00Z
updated_at: 2026-03-20T10:00:00Z
parent: tnib-f020
order: b
---

## Description

POST /tasks/batch endpoint that accepts an array of task IDs and an operation (update status, assign, move, delete).

## Verification

- [ ] Accepts up to 100 task IDs per request
- [ ] Supports operations: update_status, assign, move_to_project, delete
- [ ] Atomic: all succeed or all fail (transaction)
- [ ] Returns individual results for each task
ENDNIB

# ============================================================
# STANDALONE TASKS (no parent)
# ============================================================

cat > "$DIR/tnib-t032--setup-ci-cd-pipeline.md" << 'ENDNIB'
---
# tnib-t032
version: 2
title: Set up CI/CD pipeline
status: completed
type: task
priority: high
estimate: m
created_at: 2026-02-15T09:00:00Z
updated_at: 2026-02-22T17:00:00Z
order: c
---

## Description

GitHub Actions CI/CD pipeline with lint, test, build, and deploy stages.

## Verification

- [x] Lint: golangci-lint + eslint
- [x] Test: Go tests + Vitest
- [x] Build: Docker image pushed to registry
- [x] Deploy: Automatic deploy to staging on main merge
ENDNIB

cat > "$DIR/tnib-t033--configure-staging-environment.md" << 'ENDNIB'
---
# tnib-t033
version: 2
title: Configure staging environment
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-18T09:00:00Z
updated_at: 2026-02-25T15:00:00Z
order: d
---

## Description

Provision staging environment on AWS with PostgreSQL RDS, Redis ElastiCache, and ECS Fargate.

## Verification

- [x] Staging environment accessible at staging.taskflow.dev
- [x] Database seeded with test data
- [x] Environment variables configured via AWS Secrets Manager
- [x] Deployment verified end-to-end
ENDNIB

cat > "$DIR/tnib-t034--database-migration-strategy.md" << 'ENDNIB'
---
# tnib-t034
version: 2
title: Write database migration strategy doc
status: todo
type: task
priority: normal
estimate: m
tags:
    - docs
created_at: 2026-03-05T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
order: e
---

## Description

Document our approach to database migrations: tool choice (golang-migrate), naming conventions, rollback procedures, and zero-downtime migration patterns.

## Verification

- [ ] Migration tool documented (golang-migrate)
- [ ] Naming convention: `YYYYMMDDHHMMSS_description.sql`
- [ ] Rollback procedure for each migration type
- [ ] Zero-downtime patterns for schema changes
ENDNIB

cat > "$DIR/tnib-t035--accessibility-audit.md" << 'ENDNIB'
---
# tnib-t035
version: 2
title: Accessibility audit
status: todo
type: task
priority: low
estimate: l
tags:
    - a11y
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
order: f
---

## Description

Conduct WCAG 2.1 AA compliance audit of all UI screens. Use axe-core automated testing plus manual keyboard and screen reader testing.

## Verification

- [ ] Automated axe-core scan with zero critical violations
- [ ] All interactive elements keyboard accessible
- [ ] Screen reader navigation tested (VoiceOver + NVDA)
- [ ] Color contrast ratios meet AA standard (4.5:1 for text)
ENDNIB

cat > "$DIR/tnib-t036--security-pen-testing.md" << 'ENDNIB'
---
# tnib-t036
version: 2
title: Security penetration testing
status: todo
type: task
priority: high
estimate: xl
tags:
    - security
created_at: 2026-03-10T10:00:00Z
updated_at: 2026-03-15T10:00:00Z
blocked_by:
    - tnib-e001
order: g
---

## Description

Conduct security penetration testing covering OWASP Top 10 vulnerabilities. Must be completed before public beta launch.

## Verification

- [ ] OWASP Top 10 checklist reviewed
- [ ] SQL injection testing on all API endpoints
- [ ] XSS testing on all user input fields
- [ ] Authentication bypass attempts documented
- [ ] Report with findings and severity ratings
ENDNIB

cat > "$DIR/tnib-t037--update-npm-dependencies.md" << 'ENDNIB'
---
# tnib-t037
version: 2
title: Update npm dependencies to latest
status: in-progress
type: task
priority: normal
estimate: s
created_at: 2026-03-20T09:00:00Z
updated_at: 2026-03-25T10:00:00Z
order: h
---

## Description

Update all npm dependencies to latest compatible versions. Check for breaking changes in major version bumps.

## Verification

- [x] `npm audit` shows zero high/critical vulnerabilities
- [ ] All tests pass after update
- [ ] Visual regression check on main UI screens
ENDNIB

cat > "$DIR/tnib-t038--error-monitoring-service.md" << 'ENDNIB'
---
# tnib-t038
version: 2
title: Set up error monitoring service
status: completed
type: task
priority: normal
estimate: m
created_at: 2026-02-20T09:00:00Z
updated_at: 2026-03-01T14:00:00Z
order: i
---

## Description

Integrate Sentry for error monitoring with source maps, release tracking, and Slack alerts.

## Verification

- [x] Sentry SDK integrated in both frontend and backend
- [x] Source maps uploaded on deploy
- [x] Release tracking matches git tags
- [x] Slack channel receives alerts for new errors
ENDNIB

cat > "$DIR/tnib-t039--optimize-docker-image.md" << 'ENDNIB'
---
# tnib-t039
version: 2
title: Optimize Docker image size
status: todo
type: task
priority: low
estimate: m
tags:
    - devops
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
order: j
area: infra
---

## Description

Reduce Docker image size from ~800MB to <200MB using multi-stage builds, Alpine base, and pruning dev dependencies.

## Verification

- [ ] Multi-stage Dockerfile (build + runtime)
- [ ] Alpine or distroless base image
- [ ] Final image size <200MB
- [ ] All health checks pass on optimized image
ENDNIB

cat > "$DIR/tnib-t040--load-testing.md" << 'ENDNIB'
---
# tnib-t040
version: 2
title: Load testing with 1000 concurrent users
status: todo
type: task
priority: normal
estimate: l
tags:
    - perf
created_at: 2026-03-15T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: k
---

## Description

Load test with k6 simulating 1000 concurrent users performing typical workflows. Establish baseline metrics and find breaking points.

## Verification

- [ ] k6 test scripts for: login, list tasks, create task, update task
- [ ] Sustained 1000 concurrent users for 10 minutes
- [ ] p95 response time <500ms for all endpoints
- [ ] Error rate <0.1%
- [ ] Results documented with graphs
ENDNIB

cat > "$DIR/tnib-t043--structured-logging.md" << 'ENDNIB'
---
# tnib-t043
version: 2
title: Add structured logging
status: completed
type: task
priority: normal
estimate: m
tags:
    - observability
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-05T16:00:00Z
order: l
---

## Description

Replace fmt.Printf logging with structured JSON logging using slog. Add request IDs, user IDs, and timing to all log entries.

## Verification

- [x] slog with JSON handler configured
- [x] Request ID middleware injects trace ID
- [x] All HTTP handlers log request/response with timing
- [x] Log levels: DEBUG, INFO, WARN, ERROR used appropriately
ENDNIB

cat > "$DIR/tnib-t044--onboarding-guide.md" << 'ENDNIB'
---
# tnib-t044
version: 2
title: Write onboarding guide for new developers
status: todo
type: task
priority: normal
estimate: m
tags:
    - docs
created_at: 2026-03-18T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: m
---

## Description

Getting-started guide covering local development setup, project structure, coding conventions, PR process, and debugging tips.

## Verification

- [ ] Prerequisites section (Go 1.22+, Node 20+, Docker)
- [ ] Step-by-step local setup instructions
- [ ] Project structure overview with package responsibilities
- [ ] Common debugging scenarios and tools
ENDNIB

# ============================================================
# RESEARCH
# ============================================================

cat > "$DIR/tnib-r001--oauth2-library-evaluation.md" << 'ENDNIB'
---
# tnib-r001
version: 2
title: Evaluate OAuth2 provider libraries
status: completed
type: research
priority: normal
estimate: m
tags:
    - security
created_at: 2026-02-20T10:00:00Z
updated_at: 2026-03-01T16:00:00Z
parent: tnib-f002
order: d
---

Spike to pick the OAuth2 client library before building the social-login feature.

## Question

Which OAuth2 client library should we standardize on for Google and GitHub sign-in — do we adopt a batteries-included framework or wire up the flow against a minimal client?

## Findings

- A full auth framework handles token refresh and provider metadata out of the box, but pulls in session and CSRF opinions we would have to fight.
- A minimal client keeps the authorization-code + PKCE flow explicit and testable, at the cost of writing provider config ourselves.
- Both Google and GitHub follow standard authorization-code; no provider-specific quirks that force a heavier dependency.

## Decision

Use a minimal OAuth2 client and own the provider config. The flow is small enough that explicit control beats framework lock-in, and it keeps the token store consistent with our existing session handling.

## Follow-ups

- Feed the decision into the OAuth2 social-login feature.
- Revisit if a third provider with non-standard flow is added.
ENDNIB

cat > "$DIR/tnib-r002--concurrent-edit-conflict-strategy.md" << 'ENDNIB'
---
# tnib-r002
version: 2
title: Research concurrent task-edit conflict strategy
status: todo
type: research
priority: high
estimate: m
tags:
    - perf
created_at: 2026-03-18T09:00:00Z
updated_at: 2026-03-24T11:00:00Z
parent: tnib-f004
order: d
---

Decide how simultaneous edits to the same task should be reconciled before multi-user editing lands.

## Question

When two clients edit the same task concurrently, do we use optimistic concurrency (etag / version check, reject-and-retry) or last-write-wins, and how does the choice interact with the planned real-time collaboration work?

## Findings

- (pending) Compare optimistic version checks against field-level merge.
- (pending) Estimate conflict frequency for the expected team sizes.

## Decision

Pending.

## Follow-ups

- Align the outcome with the Real-time Collaboration epic.
ENDNIB

# ============================================================
# BUGS
#
# A bug's only legal parent is an epic (nibtypes.ValidateParentType), so a bug
# about a feature's subject matter hangs off that feature's EPIC rather than
# the feature — which still places it in the subtree a reader expects to find
# it under. Seven are parented that way; the other eight are deliberately
# parentless, covering the loose-bug shape.
# ============================================================

cat > "$DIR/tnib-b001--oauth-callback-url-mismatch.md" << 'ENDNIB'
---
# tnib-b001
version: 2
title: OAuth callback URL mismatch in production config
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T14:00:00Z
updated_at: 2026-03-22T09:00:00Z
parent: tnib-e001
order: f
---

## Steps to Reproduce

1. Deploy to production environment
2. Click "Login with Google"
3. Complete Google consent screen
4. Observe "redirect_uri_mismatch" error

## Expected vs Actual

**Expected:** User redirected back to app and logged in
**Actual:** Google returns `redirect_uri_mismatch` error because production config still has staging callback URL

## Root Cause

`OAUTH_CALLBACK_URL` environment variable in production is set to `https://staging.taskflow.dev/auth/callback` instead of `https://taskflow.dev/auth/callback`. Terraform config has the correct value but it wasn't applied after the last deploy.
ENDNIB

cat > "$DIR/tnib-b002--session-tokens-not-invalidated.md" << 'ENDNIB'
---
# tnib-b002
version: 2
title: Session tokens not invalidated on password change
status: todo
type: bug
priority: critical
estimate: m
tags:
    - security
created_at: 2026-03-15T10:00:00Z
updated_at: 2026-03-20T09:00:00Z
parent: tnib-e001
order: e
area: auth
---

## Steps to Reproduce

1. Log in on Device A
2. Change password on Device A
3. Check Device B — still logged in with old session

## Expected vs Actual

**Expected:** All other sessions invalidated after password change
**Actual:** Existing sessions remain valid indefinitely

## Root Cause

Password change handler updates the password hash but doesn't increment the session generation counter. The session validation middleware doesn't check generation — it only validates the session token exists in Redis.
ENDNIB

cat > "$DIR/tnib-b003--deactivated-user-assignment.md" << 'ENDNIB'
---
# tnib-b003
version: 2
title: Assigning task to deactivated user shows no error
status: in-progress
type: bug
priority: high
estimate: s
created_at: 2026-03-20T09:00:00Z
updated_at: 2026-03-24T15:00:00Z
parent: tnib-e002
order: g
---

## Steps to Reproduce

1. Deactivate user "alice@example.com" via admin panel
2. Assign a task to "alice@example.com" via API
3. Request succeeds with 200 OK

## Expected vs Actual

**Expected:** 400 Bad Request with message "Cannot assign to deactivated user"
**Actual:** Assignment succeeds silently; task shows as assigned to a ghost user

## Root Cause

Assignment endpoint only checks if user ID exists in users table, doesn't check `is_active` flag. Need to add `WHERE is_active = true` to the user lookup query.
ENDNIB

cat > "$DIR/tnib-b004--task-title-truncation.md" << 'ENDNIB'
---
# tnib-b004
version: 2
title: Task title silently truncated at 255 characters
status: todo
type: bug
priority: low
estimate: s
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e002
order: f
---

## Steps to Reproduce

1. Create a task with a title longer than 255 characters via API
2. Read the task back

## Expected vs Actual

**Expected:** Either accept full title (up to our stated 500 char limit) or return 400 if too long
**Actual:** Title silently truncated to 255 chars by database column constraint

## Root Cause

Database column is `VARCHAR(255)` but API validation allows up to 500 chars. Either widen the column to 500 or lower the API limit to 255.
ENDNIB

cat > "$DIR/tnib-b005--date-picker-timezone.md" << 'ENDNIB'
---
# tnib-b005
version: 2
title: Date picker shows wrong timezone for remote users
status: todo
type: bug
priority: normal
estimate: m
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e003
order: d
area: web
---

## Steps to Reproduce

1. Set system timezone to UTC+9 (Tokyo)
2. Open task with due date "2026-03-15"
3. Date picker shows "March 14" (off by one due to UTC conversion)

## Expected vs Actual

**Expected:** Date picker shows the date as entered, in user's local timezone
**Actual:** Date displayed relative to UTC, causing off-by-one for users east of UTC

## Root Cause

Dates stored as UTC timestamps but rendered without timezone conversion. Should store as date-only (no time component) or convert to user's timezone on display.
ENDNIB

cat > "$DIR/tnib-b006--webhook-missing-updated-at.md" << 'ENDNIB'
---
# tnib-b006
version: 2
title: Webhook payloads missing updated_at timestamp
status: todo
type: bug
priority: normal
estimate: s
created_at: 2026-03-10T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e004
order: d
---

## Steps to Reproduce

1. Register a webhook for task.updated events
2. Update a task
3. Inspect the webhook payload

## Expected vs Actual

**Expected:** Payload includes `updated_at` timestamp
**Actual:** `updated_at` field is missing from the payload

## Root Cause

Webhook serializer uses `TaskSummary` struct which doesn't include `UpdatedAt` field. Need to add it to the struct and the JSON serialization.
ENDNIB

cat > "$DIR/tnib-b007--push-notification-permission.md" << 'ENDNIB'
---
# tnib-b007
version: 2
title: Push notifications not requesting permission on mobile
status: draft
type: bug
priority: low
estimate: s
created_at: 2026-03-14T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e006
order: c
---

## Steps to Reproduce

1. Open app on mobile Safari (iOS 16+)
2. No permission prompt appears for push notifications
3. Notification settings show "Not Determined"

## Expected vs Actual

**Expected:** Permission prompt shown on first relevant action (e.g., enabling notifications)
**Actual:** Permission never requested because Service Worker registration fails silently on mobile Safari

## Root Cause

Service Worker scope is misconfigured — needs investigation into Safari PWA requirements.
ENDNIB

cat > "$DIR/tnib-b008--dev-server-memory-leak.md" << 'ENDNIB'
---
# tnib-b008
version: 2
title: Memory leak in development server after hot reload
status: in-progress
type: bug
priority: critical
estimate: s
created_at: 2026-03-22T09:00:00Z
updated_at: 2026-03-25T14:00:00Z
order: "n"
---

## Steps to Reproduce

1. Start development server with `npm run dev`
2. Make 20+ file changes triggering hot reload
3. Observe Node.js memory usage climbing from ~150MB to 1.2GB+

## Expected vs Actual

**Expected:** Memory usage stable around 200-300MB regardless of hot reloads
**Actual:** Memory grows ~50MB per hot reload and is never reclaimed

## Root Cause

Vite's HMR is not cleaning up old module instances. Likely related to circular dependency in the state management layer preventing garbage collection. Investigating with `--inspect` heap snapshots.
ENDNIB

cat > "$DIR/tnib-b009--flaky-user-service-test.md" << 'ENDNIB'
---
# tnib-b009
version: 2
title: Flaky test in user_service_test.go
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T09:00:00Z
updated_at: 2026-03-22T10:00:00Z
order: o
---

## Steps to Reproduce

1. Run `go test ./internal/service/ -count=100`
2. TestUserService_ConcurrentUpdate fails ~5% of the time

## Expected vs Actual

**Expected:** Test passes consistently
**Actual:** Intermittent failure with "context deadline exceeded" — race condition in test setup

## Root Cause

Test uses a shared database connection pool without proper per-test isolation. When tests run in parallel, the pool is exhausted and some tests timeout waiting for a connection. Need to use `t.Parallel()` with per-test transactions.
ENDNIB

cat > "$DIR/tnib-b010--safari-grid-layout.md" << 'ENDNIB'
---
# tnib-b010
version: 2
title: CSS grid layout breaks on Safari 15
status: todo
type: bug
priority: normal
estimate: m
tags:
    - browser-compat
created_at: 2026-03-15T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: p
---

## Steps to Reproduce

1. Open task board in Safari 15.x
2. Observe columns overlapping when board has >5 columns

## Expected vs Actual

**Expected:** Columns sized correctly with horizontal scroll when needed
**Actual:** Columns overlap because Safari 15 doesn't support `subgrid` — falls back to `auto` sizing

## Root Cause

We use `grid-template-columns: subgrid` which Safari didn't support until 16.0. Need fallback for Safari 15 using explicit column definitions.
ENDNIB

cat > "$DIR/tnib-b011--api-500-malformed-json.md" << 'ENDNIB'
---
# tnib-b011
version: 2
title: API returns 500 on malformed JSON body
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T10:00:00Z
updated_at: 2026-03-22T09:00:00Z
order: q
area: api
---

## Steps to Reproduce

1. Send POST /api/tasks with body: `{invalid json`
2. Server returns 500 Internal Server Error

## Expected vs Actual

**Expected:** 400 Bad Request with message "Invalid JSON: unexpected character at position 1"
**Actual:** 500 Internal Server Error with generic message — json.Decoder error is not caught by error middleware

## Root Cause

Error middleware only handles our custom `AppError` type. stdlib `json.SyntaxError` and `json.UnmarshalTypeError` fall through to the default 500 handler. Need to add type switches in the error middleware.
ENDNIB

cat > "$DIR/tnib-b012--search-stale-after-delete.md" << 'ENDNIB'
---
# tnib-b012
version: 2
title: Search results not updating after task deletion
status: in-progress
type: bug
priority: normal
estimate: s
created_at: 2026-03-20T14:00:00Z
updated_at: 2026-03-25T11:00:00Z
order: u
---

## Steps to Reproduce

1. Create a task with title "quarterly review"
2. Delete the task
3. Search for "quarterly" — deleted task still appears in results

## Expected vs Actual

**Expected:** Deleted tasks excluded from search results immediately
**Actual:** Search index retains deleted tasks until server restart

## Root Cause

Delete handler removes from database but doesn't update the Bleve search index. Need to call `index.Delete(taskID)` in the delete path.
ENDNIB

cat > "$DIR/tnib-b013--file-upload-special-chars.md" << 'ENDNIB'
---
# tnib-b013
version: 2
title: File upload fails for names with special characters
status: todo
type: bug
priority: high
estimate: s
tags:
    - i18n
created_at: 2026-03-20T15:00:00Z
updated_at: 2026-03-22T09:00:00Z
order: v
---

## Steps to Reproduce

1. Attach a file named `报告_2026年.pdf` to a task
2. Upload fails with "Invalid filename" error

## Expected vs Actual

**Expected:** File uploaded successfully with Unicode filename preserved
**Actual:** Filename validation regex `^[a-zA-Z0-9._-]+$` rejects non-ASCII characters

## Root Cause

Overly restrictive filename validation. Should sanitize path separators and null bytes but allow Unicode characters.
ENDNIB

cat > "$DIR/tnib-b014--race-condition-concurrent-updates.md" << 'ENDNIB'
---
# tnib-b014
version: 2
title: Race condition in concurrent task updates
status: todo
type: bug
priority: critical
estimate: l
created_at: 2026-03-22T09:00:00Z
updated_at: 2026-03-25T10:00:00Z
order: w
---

## Steps to Reproduce

1. Open the same task in two browser tabs
2. Change status to "done" in tab A
3. Immediately change assignee in tab B (before tab A's change is reflected)
4. Tab B's update overwrites tab A's status change

## Expected vs Actual

**Expected:** Optimistic concurrency detects the conflict and prompts user to resolve
**Actual:** Last write wins — tab B's update silently reverts tab A's status change

## Root Cause

API uses PUT (full replace) instead of PATCH (partial update). No ETag/If-Match headers for optimistic concurrency control. Need to implement either:
1. ETags with If-Match headers (preferred)
2. Field-level versioning
ENDNIB

cat > "$DIR/tnib-b015--dropdown-zindex-modal.md" << 'ENDNIB'
---
# tnib-b015
version: 2
title: Dropdown menu z-index conflict with modal
status: todo
type: bug
priority: normal
estimate: s
tags:
    - ux
created_at: 2026-03-22T10:00:00Z
updated_at: 2026-03-25T09:00:00Z
order: x
---

## Steps to Reproduce

1. Open the task edit modal
2. Click the priority dropdown inside the modal
3. Dropdown renders behind the modal backdrop

## Expected vs Actual

**Expected:** Dropdown floats above the modal
**Actual:** Dropdown is clipped by modal's `overflow: hidden` and renders behind the backdrop

## Root Cause

Dropdown uses `position: absolute` relative to the modal, which has `overflow: hidden`. Need to portal the dropdown to `document.body` or use Floating UI for proper positioning.
ENDNIB

echo ""
echo "Generated $(find "$DIR" -name '*.md' | wc -l) nib files in $DIR"
echo ""
# --nibs-path names the store, and the store carries its own config, so the
# fixture is read under its own prefix wherever the command is run from.
echo "To use this fixture:"
echo "  nibs list --nibs-path testdata/fixtures/sample-project/.nibs"
echo "  nibs serve --nibs-path testdata/fixtures/sample-project/.nibs"

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

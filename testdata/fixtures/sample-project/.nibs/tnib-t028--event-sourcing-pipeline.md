---
# tnib-t028
version: 1
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

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

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

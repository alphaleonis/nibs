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

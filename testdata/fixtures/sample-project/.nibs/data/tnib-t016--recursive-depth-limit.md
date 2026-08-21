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

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

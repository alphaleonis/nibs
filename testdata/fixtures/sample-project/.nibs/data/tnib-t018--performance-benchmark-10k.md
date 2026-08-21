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

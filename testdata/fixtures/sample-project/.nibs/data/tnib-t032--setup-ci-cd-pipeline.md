---
# tnib-t032
version: 1
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

---
# tnib-t039
version: 1
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
---

## Description

Reduce Docker image size from ~800MB to <200MB using multi-stage builds, Alpine base, and pruning dev dependencies.

## Verification

- [ ] Multi-stage Dockerfile (build + runtime)
- [ ] Alpine or distroless base image
- [ ] Final image size <200MB
- [ ] All health checks pass on optimized image

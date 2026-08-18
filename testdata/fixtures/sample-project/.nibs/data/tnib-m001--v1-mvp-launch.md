---
# tnib-m001
version: 1
title: v1.0 MVP Launch
status: in-progress
type: milestone
priority: critical
created_at: 2026-02-15T09:00:00Z
updated_at: 2026-03-25T14:30:00Z
documents:
    - docs/product-roadmap.md
order: a
---

Ship the minimum viable product with core task management, user authentication, basic dashboard, and API foundation.

## Goal

Deliver a working product that early adopters can use for daily project management by end of Q1 2026.

## Current Focus

- Completing OAuth2 authentication flow
- Stabilizing task management CRUD operations
- Addressing critical security bugs before beta

## Key Decisions

- Chose PostgreSQL over MongoDB for ACID compliance requirements
- REST API first, GraphQL deferred to v1.2
- Server-side rendering deferred — SPA with Svelte for now

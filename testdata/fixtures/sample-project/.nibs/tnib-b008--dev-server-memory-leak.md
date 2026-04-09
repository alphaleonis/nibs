---
# tnib-b008
version: 1
title: Memory leak in development server after hot reload
status: in-progress
type: bug
priority: critical
estimate: s
created_at: 2026-03-22T09:00:00Z
updated_at: 2026-03-25T14:00:00Z
order: n
---

## Steps to Reproduce

1. Start development server with `npm run dev`
2. Make 20+ file changes triggering hot reload
3. Observe Node.js memory usage climbing from ~150MB to 1.2GB+

## Expected vs Actual

**Expected:** Memory usage stable around 200-300MB regardless of hot reloads
**Actual:** Memory grows ~50MB per hot reload and is never reclaimed

## Root Cause

Vite's HMR is not cleaning up old module instances. Likely related to circular dependency in the state management layer preventing garbage collection. Investigating with `--inspect` heap snapshots.

---
# tnib-b015
version: 2
title: Dropdown menu z-index conflict with modal
status: todo
type: bug
priority: normal
estimate: s
tags:
    - ux
created_at: 2026-03-22T10:00:00Z
updated_at: 2026-03-25T09:00:00Z
order: x
---

## Steps to Reproduce

1. Open the task edit modal
2. Click the priority dropdown inside the modal
3. Dropdown renders behind the modal backdrop

## Expected vs Actual

**Expected:** Dropdown floats above the modal
**Actual:** Dropdown is clipped by modal's `overflow: hidden` and renders behind the backdrop

## Root Cause

Dropdown uses `position: absolute` relative to the modal, which has `overflow: hidden`. Need to portal the dropdown to `document.body` or use Floating UI for proper positioning.

---
# tnib-b013
version: 1
title: File upload fails for names with special characters
status: todo
type: bug
priority: high
estimate: s
tags:
    - i18n
created_at: 2026-03-20T15:00:00Z
updated_at: 2026-03-22T09:00:00Z
order: v
---

## Steps to Reproduce

1. Attach a file named `报告_2026年.pdf` to a task
2. Upload fails with "Invalid filename" error

## Expected vs Actual

**Expected:** File uploaded successfully with Unicode filename preserved
**Actual:** Filename validation regex `^[a-zA-Z0-9._-]+$` rejects non-ASCII characters

## Root Cause

Overly restrictive filename validation. Should sanitize path separators and null bytes but allow Unicode characters.

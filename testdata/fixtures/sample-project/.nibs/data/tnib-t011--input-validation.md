---
# tnib-t011
version: 2
title: Add input validation and sanitization
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-06T14:00:00Z
parent: tnib-f004
order: c
---

## Description

Validate all task input fields: title length, description size, valid enum values for status/priority, XSS sanitization.

## Verification

- [x] Title: 1-500 chars, trimmed
- [x] Description: max 50KB, HTML sanitized
- [x] Status and priority: validated against allowed values
- [x] Error messages include field name and constraint

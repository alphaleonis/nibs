---
# tnib-t031
version: 1
title: Build email template system
status: draft
type: task
priority: normal
estimate: m
created_at: 2026-03-14T12:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f017
order: a
---

## Description

Templated email system using Go html/template with a base layout, reusable components, and per-notification-type templates.

## Verification

- [ ] Base email layout with header, content area, footer
- [ ] Templates for: assignment, mention, deadline, digest
- [ ] Preview endpoint for development (renders template with sample data)
- [ ] Plain text and HTML versions for each template

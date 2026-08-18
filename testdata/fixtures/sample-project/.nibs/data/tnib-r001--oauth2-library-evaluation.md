---
# tnib-r001
version: 1
title: Evaluate OAuth2 provider libraries
status: completed
type: research
priority: normal
estimate: m
tags:
    - security
created_at: 2026-02-20T10:00:00Z
updated_at: 2026-03-01T16:00:00Z
parent: tnib-f002
order: d
---

Spike to pick the OAuth2 client library before building the social-login feature.

## Question

Which OAuth2 client library should we standardize on for Google and GitHub sign-in — do we adopt a batteries-included framework or wire up the flow against a minimal client?

## Findings

- A full auth framework handles token refresh and provider metadata out of the box, but pulls in session and CSRF opinions we would have to fight.
- A minimal client keeps the authorization-code + PKCE flow explicit and testable, at the cost of writing provider config ourselves.
- Both Google and GitHub follow standard authorization-code; no provider-specific quirks that force a heavier dependency.

## Decision

Use a minimal OAuth2 client and own the provider config. The flow is small enough that explicit control beats framework lock-in, and it keeps the token store consistent with our existing session handling.

## Follow-ups

- Feed the decision into the OAuth2 social-login feature.
- Revisit if a third provider with non-standard flow is added.

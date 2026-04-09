---
# tnib-t033
version: 1
title: Configure staging environment
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-18T09:00:00Z
updated_at: 2026-02-25T15:00:00Z
order: d
---

## Description

Provision staging environment on AWS with PostgreSQL RDS, Redis ElastiCache, and ECS Fargate.

## Verification

- [x] Staging environment accessible at staging.taskflow.dev
- [x] Database seeded with test data
- [x] Environment variables configured via AWS Secrets Manager
- [x] Deployment verified end-to-end

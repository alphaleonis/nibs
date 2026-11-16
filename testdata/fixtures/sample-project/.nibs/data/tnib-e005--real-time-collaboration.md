---
# tnib-e005
version: 2
title: Real-time Collaboration
status: draft
type: epic
priority: normal
estimate: xl
created_at: 2026-03-10T10:00:00Z
updated_at: 2026-03-18T09:00:00Z
documents:
    - docs/websocket-rfc.md
milestone: tnib-m002
milestone_order: a
---

## Summary

Today two people looking at the same board see two different boards until one of them reloads. That was acceptable while teams were small, but the median team now has seven members, and the most common complaint in the v1.0 beta is "someone moved my task and I didn't see it happen". This epic makes the board live: changes made by anyone appear for everyone within a second, edits to the same task do not silently overwrite each other, and an activity feed explains who changed what.

It is deliberately the first epic of v1.1 ([[tnib-m002]]), because the dashboard's real-time updates in #tnib-e003 and the notification fan-out in #tnib-e006 both reuse the same event stream. The transport is specified in `docs/websocket-rfc.md`.

## Problem

Collaboration breaks down in three places:

1. **Stale views.** A board is fetched once and never refreshed, so people act on information that is minutes old.
2. **Lost updates.** Two people editing the same task both save, and the second save wins without either of them knowing.
3. **No history.** When a task changes unexpectedly there is no way to see who changed it, or what it looked like before.

> Beta survey, open question "What slows your team down?": 41 of 112 answers mention seeing out-of-date tasks or edits disappearing.

## Goals and non-goals

**Goals**

- Push every task change to connected clients over a WebSocket ([[tnib-f014]]), with automatic reconnect and catch-up after a dropped connection.
- Detect conflicting edits with per-task version numbers, and show the losing editor what changed instead of discarding their work.
- An activity feed per project and per task, built from the same events ([[tnib-f015]]).

**Non-goals**

- Character-by-character co-editing of descriptions. That needs operational transforms or CRDTs, which is a project of its own.
- Presence indicators ("Alex is viewing this task"). Cheap to add on top, but not needed to fix the problems above.
- Voice or video.

## User stories

- As a team member, when a colleague moves a task to "Done", I see it move on my screen without reloading.
- As someone editing a task, if a colleague saves a change to the same task first, I am shown their change next to mine and choose what to keep.
- As a team lead, I can open a task and read what happened to it, newest first, with who did it and when.
- As a user on a flaky train connection, my board catches up by itself when the connection returns, without duplicating anything.

## Requirements

| Area | Requirement | Notes |
|---|---|---|
| Transport | WebSocket with JSON frames; fall back to long polling where WebSockets are blocked | Protocol per [RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455) |
| Delivery | At-least-once, de-duplicated on the client by event id | Ordering guaranteed per task, not globally |
| Latency | Change visible to other clients within 1 s at the 95th percentile | Measured from commit to render |
| Reconnect | Resume from the last event id within a 24 h retention window; full refetch beyond it | Backoff 1 s doubling to 30 s |
| Conflicts | Every task carries a version; a save with a stale version is rejected with the current state | The client shows a merge view |
| Feed | Events retained for 90 days; feed pages of 50 | Feed rows link to the task and the actor |
| Scale | 5,000 concurrent connections per node; horizontal scaling through a shared pub/sub | Load test before beta |

## Design notes

Every mutation already passes through one service layer, so it writes an event row in the same transaction as the change ("transactional outbox"). A relay reads the outbox and publishes to the pub/sub channel for the project; each WebSocket node subscribes to the projects its clients have open. The pattern is described in [Microservices.io: Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html).

```text
client ──save──▶ API ──tx──▶ tasks + outbox
                              │
                       relay ─┴─▶ pub/sub (project:42) ──▶ ws node ──▶ clients
```

A frame on the wire looks like this:

```json
{"id": 90231, "type": "task.moved", "task": "tsk_7d1", "version": 14,
 "actor": "user_8f3a", "at": "2026-05-04T09:12:44Z",
 "change": {"status": ["in-progress", "done"]}}
```

Conflict handling is optimistic: the client applies its own change at once and keeps the previous state; if the server rejects the save because the version moved, the client rolls back, applies the server's state, and opens the merge view with both versions side by side.

## Rollout

1. Run the event outbox and relay in production with no consumers for two weeks, to measure volume.
2. Enable live updates for internal teams, then for 10% of projects, watching connection counts and reconnect storms.
3. Ship conflict detection and the merge view together; neither is useful alone.
4. Turn on the activity feed last, once 90 days of events have accumulated for internal projects.

## Risks

- **Reconnect storms** after a deploy can overwhelm the nodes. Mitigation: randomized backoff and a drain period before restart.
- **Proxies that kill idle connections** cause silent staleness; send a heartbeat every 25 seconds.
- **Event volume** from bulk operations (#tnib-f020 lets one action touch hundreds of tasks) could flood clients; coalesce bulk changes into one event per batch.

## Objective

Enable live multi-user editing with WebSocket-based updates and conflict resolution.

## Acceptance Criteria

- [ ] WebSocket server with connection management
- [ ] Optimistic UI updates with server reconciliation
- [ ] Activity feed powered by event sourcing

## Scope Boundaries

- Operational transform / CRDT for rich text editing deferred
- Voice/video collaboration out of scope

## Open questions

- Is long polling worth building, or do we tell blocked networks to use reload?
- Should the merge view also appear when the conflicting change came from the same user on another device?

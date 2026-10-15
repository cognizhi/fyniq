# Design

## Context

ADR-T7 (accepted, rev 0.21) and §12.11 define the contract; §12.8 names River as the local adapter. The
unit of work (p0-database-tenancy-migrations) gives the transaction the outbox row joins.

## Goals / Non-Goals

**Goals:**
- No job lost and no phantom job: the outbox row commits or rolls back with the change.
- The same contract and the same conformance suite for Cloud Tasks and River.

**Non-Goals:**
- Business schedules (recurring documents, reminders) and connectors, which arrive in P1 on top of this.

## Decisions

- **Outbox first, dispatch after commit.** The API enqueues a task named after the outbox row ID, so a
  duplicate enqueue is rejected by Cloud Tasks' name de-duplication. The tick sweeps rows that committed
  but never dispatched.
- **Leases, not locks.** A handler takes a lease (holder and expiry) on its row; an expired lease is
  swept and retried. Handlers are idempotent on the outbox ID.
- **Unknown outcome is a state.** A time-out after sending marks the row unknown-outcome, and the next
  step is reconciliation with the provider, never a blind retry.
- **Worker classes.** `worker` (general), `executor` (approved proposals only) and jobs. Each has its own
  queue, service account and OIDC audience, and only the `api` account can create executor tasks.
- **Heartbeat.** A custom metric per run, with a Cloud Monitoring absence alert (local: a log line and a
  test).
- **Audit events emitted.** `outbox.row.dead` when retries are exhausted, and `schedule.slot.skipped`
  for missed slots.
- **Ports and adapters.** `TaskQueue` (Cloud Tasks, River), `JobRunner` (Cloud Run jobs, local runner)
  and `Clock`.

## Risks / Trade-offs

- [Cloud Tasks' de-duplication window is limited] → the outbox row ID and the handler's idempotency
  carry the guarantee; the task name only reduces duplicates.
- [The tick is a single point of failure] → the absence alert pages when it stops, and missed slots are
  run once on recovery.

## Implementation notes (15 Oct 2026)

- **Local adapter: a push dispatcher, not River** (founder decision, PRD rev 0.23). It POSTs each task
  to its worker class's `/tasks` endpoint with that class's token, Cloud Tasks' headers, a deadline
  per attempt and retries with backoff, and accepts a task name once. Locally the handlers, the token
  check, leases and retries therefore run exactly as in production. River stays the self-hosted
  option. The in-memory queue remains the test double.
- **Ports.** `ports.Queue` is now push-only (`Enqueue`); `ports.TaskTokens` issues and checks each
  class's credential (local: HMAC tokens with an expiry; production: Cloud Tasks' OIDC tokens, checked
  by audience in the GCP adapter). `storage.UnitOfWork` gained `Outbox()` and `AfterCommit`.
- **Dispatch after commit.** `outbox.Dispatcher.Enqueue` writes the row in the command's unit of work
  and registers an after-commit hook; the hook marks the row dispatched (task name = row ID + dispatch
  number, deadline) and then enqueues it, so a delivery never finds its row still pending. A failed
  enqueue leaves the row overdue for the sweep.
- **Handlers.** `outbox.Handler` checks the class's credential (403 otherwise), takes the lease in its
  own transaction, runs the work in the work's own unit of work, and finishes the row: done, failed
  (500, so the queue retries), unknown_outcome (200; reconciliation first) or dead after the last
  attempt (200, with an `outbox.row.dead` audit event for Checks). A row already done, dead or waiting
  on reconciliation answers 200 without running; one leased by another delivery answers 409. A row
  delivered to the wrong class is parked as dead, never run. `outbox.Once` turns a repeated posting
  (its posting identity derived from the row) into success, so a posting runs at most once.
- **Split into two pull requests.** This one: the outbox table and contract, dispatch, tokens, the
  handler, and their conformance and integration tests (tasks 1.1 and 1.3; 1.2 and 3.2 locally).
  Next: the tick, platform jobs, heartbeats, missed slots and overlaps, the restore command, and the
  services' wiring (tasks 2.x, 3.1).
- **Waits for p0-infra-opentofu:** the Cloud Tasks and Cloud Run jobs adapters run the same
  conformance suite in staging (1.2); queue IAM, meaning the executor's own service account and only
  the `api` account creating its tasks (3.2's cloud half); scale to zero (3.3).

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

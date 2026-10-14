# Proposal

## Why

Everything except the database scales to zero, so background work cannot rely on a process that stays
running (ADR-T7, golden rule 9). P0 builds the transactional outbox, the dispatch path and the
Scheduler tick, so that the audit anchor, the nightly verification and every later job run the same way
(PRD-001 §12.11).

## What Changes

- The transactional outbox: work is written as an outbox row in the same transaction as the change, with
  the row contract (state, attempts, lease, business key, Cloud Task name, job execution).
- Dispatch after commit through a `TaskQueue` port: a Cloud Tasks adapter (one queue per worker class,
  OIDC token per class) and the local River adapter, both passing one outbox conformance suite.
- Short work in request handlers with an explicit dispatch deadline; long work as Cloud Run jobs
  started by a task (local: a job runner in Compose).
- One Scheduler tick (every 5 minutes by default) that enqueues due schedules, sweeps the outbox and
  watches for late or failed runs; missed slots run once for the latest slot, and runs never overlap.
- Heartbeats from the tick, the nightly verification and the anchoring runs, with an absence alert
  outside the request path.
- Idempotent handlers keyed on the outbox ID; postings at most once through the posting identity;
  unknown outcomes reconciled before any retry; after a restore, unfinished rows become unknown-outcome.
- The proposal executor's own queue and token, created now so the separation holds from the start.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds the sixteen requirements of `platform/background-work` (§12.11,
ADR-T7), including the outbox conformance suite (NFR-PORT-04).

## Impact

- `internal/outbox`, `internal/ports/taskqueue`, `internal/adapters/{gcp,local}/taskqueue`, the tick
  endpoint in `cmd/api`, `cmd/worker` handlers and jobs; the Cloud Tasks queues and the Scheduler job
  are created in p0-infra-opentofu.
- **Second human reviewer recommended:** the executor queue isolation protects the AI write path.

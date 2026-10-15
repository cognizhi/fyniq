# platform/background-work Specification

## Purpose
How Fyniq runs background work while everything but the database scales to zero: the transactional outbox and its contract, Cloud Tasks dispatch per worker class, short work in request handlers and long work as Cloud Run jobs, one Scheduler tick with a watchdog and an independent heartbeat, scheduling that never depends on the database's own scheduler, and what stays on. Derived from PRD-001 §12.11 (ADR-T7).

## Requirements

### Requirement: Transactional outbox
A change SHALL write its background work as an outbox row in the same database transaction, so that no job is lost and no phantom job is created.

Source: PRD-001 §12.11

#### Scenario: Committed change has its outbox row
- **WHEN** a change that needs background work commits
- **THEN** its outbox row is committed in the same transaction

#### Scenario: Rolled-back change leaves no job
- **WHEN** a change that needs background work rolls back
- **THEN** no outbox row from it remains, so no phantom job runs

### Requirement: Dispatch through Cloud Tasks per worker class
After commit, the API SHALL enqueue a Cloud Task named after the outbox row, on one queue per worker class, with an OIDC token for that class's service.

Source: PRD-001 §12.11

#### Scenario: Task named after the outbox row
- **WHEN** the API enqueues the same outbox row twice within 24 hours
- **THEN** Cloud Tasks deduplicates the named task, so it runs once

#### Scenario: One queue and identity per worker class
- **WHEN** a task is enqueued for a worker class
- **THEN** it goes to that class's queue with an OIDC token for that class's service

#### Scenario: Pub/Sub for fan-out only
- **WHEN** work needs fan-out (P2)
- **THEN** Pub/Sub push subscriptions are used for fan-out only, not for per-task scheduling or naming

### Requirement: Proposal executor queue isolation
The `proposal-executor` SHALL have its own queue and accept only its own queue's token, and only the `api` service account SHALL create tasks that use the executor's invoker account.

Source: PRD-001 §12.11

#### Scenario: Token from another queue refused
- **WHEN** the proposal executor receives a task with a token that is not its own queue's
- **THEN** it refuses the task

#### Scenario: Only the api service account may enqueue
- **WHEN** the configuration is checked daily
- **THEN** only the `api` service account is enqueuer on the executor's queue and has actAs on the executor's invoker account, because the token proves the service account and audience, not the queue

#### Scenario: Executor runs only approved work
- **WHEN** the proposal executor receives a task
- **THEN** it runs only work it finds approved in the database (§9.3)

### Requirement: Short work in request handlers
Short work (under about 5 minutes: notifications, connector calls, postings) SHALL run in HTTP handlers on Cloud Run services at min instances 0, with the request held open for the whole run and an explicit dispatch deadline.

Source: PRD-001 §12.11

#### Scenario: Request held open for the whole run
- **WHEN** a task for short work is delivered
- **THEN** the handler holds the request open until the work is finished

#### Scenario: Explicit dispatch deadline
- **WHEN** a task for short work is enqueued
- **THEN** it carries an explicit dispatch deadline within what Cloud Tasks allows (15 seconds to 30 minutes)

### Requirement: Long work as Cloud Run jobs
Long work (agent runs, imports, exports, compiles, nightly verification) SHALL run as Cloud Run jobs (task timeout up to 7 days), started by a task.

Source: PRD-001 §12.11

#### Scenario: Long work started by a task
- **WHEN** long work is due
- **THEN** a task starts a Cloud Run job that runs it

#### Scenario: Pattern ports to other clouds
- **WHEN** the platform runs on AWS or Azure
- **THEN** long work runs as ECS RunTask on Fargate or Container Apps jobs, which cannot hold long requests open (Lambda stops at 15 minutes; Container Apps ingress at 240 seconds)

### Requirement: Scheduler tick
One Cloud Scheduler tick (every 5 minutes by default; a setting) SHALL wake the API to enqueue due schedules, sweep the outbox and act as the watchdog for late or failed runs.

Source: PRD-001 §12.11

#### Scenario: Due schedules enqueued at their time
- **WHEN** the tick runs
- **THEN** it enqueues due schedules (agents, recurring documents, bank pulls) as tasks at their exact time, as Cloud Tasks accepts times up to 30 days ahead
- **AND** "exact time" means within one tick interval

#### Scenario: Outbox sweep
- **WHEN** the tick runs
- **THEN** it sweeps the outbox for rows never enqueued and enqueues them

#### Scenario: Watchdog
- **WHEN** the tick finds a run late or failed
- **THEN** it acts as the watchdog for that run

#### Scenario: Next slot enqueued
- **WHEN** a scheduled run finishes
- **THEN** it enqueues the schedule's next slot

#### Scenario: Few fixed Scheduler jobs
- **WHEN** the Scheduler jobs are counted
- **THEN** only a few fixed jobs exist (the tick, nightly verification, audit anchoring), well inside the 5,000-jobs-per-region quota, at $0.10 per job a month with 3 free per billing account

### Requirement: Heartbeat outside the request path
Each successful tick, nightly verification and anchoring run SHALL write a heartbeat metric, and a Cloud Monitoring absence alert outside the request path (NFR-OBS-02) SHALL page the platform owner if it stops.

Source: PRD-001 §12.11

#### Scenario: Heartbeat written
- **WHEN** a tick, a nightly verification or an anchoring run succeeds
- **THEN** it writes a heartbeat metric

#### Scenario: Absence alert
- **WHEN** the heartbeat stops
- **THEN** a Cloud Monitoring absence alert pages the platform owner, without depending on the watchdog, which cannot see its own absence

### Requirement: Missed slots and overlapping runs
After a gap, each schedule SHALL run once for its latest missed slot and record the skipped slots, and two runs of one schedule SHALL never overlap.

Source: PRD-001 §12.11

#### Scenario: Catch-up after a gap
- **GIVEN** a schedule missed several slots during a gap
- **WHEN** scheduling resumes
- **THEN** the schedule runs once, for its latest missed slot
- **AND** the skipped slots are recorded

#### Scenario: Per-schedule lease
- **GIVEN** a run of a schedule holds the schedule's lease
- **WHEN** another run of the same schedule would start
- **THEN** it does not start while the lease is held

### Requirement: Scheduling never depends on the database's scheduler
Anything the business relies on SHALL be triggered by the tick, never by the database's own scheduler; `pg_cron` MAY run maintenance.

Source: PRD-001 §12.11

#### Scenario: Business work triggered by the tick
- **WHEN** work the business relies on is scheduled
- **THEN** it is triggered by the tick, because a paused serverless database skips its own cron jobs

#### Scenario: pg_cron for maintenance
- **WHEN** `pg_cron` runs a job
- **THEN** it is maintenance that nothing the business relies on depends on

### Requirement: Outbox row contract
Each outbox row SHALL have a state, an attempt count, a lease (holder and expiry), a business key, and the Cloud Task name and Cloud Run job execution it used.

Source: PRD-001 §12.11

#### Scenario: Row states
- **WHEN** an outbox row moves through its life
- **THEN** its state goes pending → dispatched → running → done, failed, unknown-outcome or dead

#### Scenario: Lease before work
- **WHEN** a handler picks up an outbox row
- **THEN** it takes the row's lease in its own transaction before doing any work

#### Scenario: Cloud Tasks retries reuse the task
- **WHEN** Cloud Tasks retries the HTTP delivery
- **THEN** the retry reuses the same task under the queue's retry settings, which have a stated maximum

#### Scenario: Platform re-dispatch uses a new task name
- **WHEN** the platform re-dispatches a row itself
- **THEN** it uses a new task name (row ID + dispatch number), because Cloud Tasks remembers names for 24 hours

#### Scenario: Sweep catches rows past their deadline
- **WHEN** a row is dispatched or running past its deadline
- **THEN** the tick's sweep picks it up, because Cloud Tasks deletes a task when its retries run out

#### Scenario: Long job reports completion
- **WHEN** a Cloud Run job started for a row finishes
- **THEN** it reports completion back to its row
- **AND** a launched job is not counted as done

#### Scenario: Dead rows on Checks
- **WHEN** a row reaches the retry limit
- **THEN** its state is dead
- **AND** it appears on Checks (C.2 #12) with a manual replay

### Requirement: Unknown outcomes of outside calls
A call to an outside service whose result is unknown (a time-out after sending) SHALL become unknown-outcome and SHALL be reconciled from the provider before any retry or failover.

Source: PRD-001 §12.11

#### Scenario: Time-out after sending
- **WHEN** a call to an outside service times out after the request was sent
- **THEN** the row's state becomes unknown-outcome
- **AND** the outcome is reconciled from the provider (status API or webhook) before any retry or failover

#### Scenario: No blind retry without idempotency
- **GIVEN** a provider has no idempotency (ZeptoMail)
- **WHEN** a call to it has an unknown outcome
- **THEN** it is never retried blind

### Requirement: Posting runs at most once
A handler that posts to the ledger SHALL write the row's business key under a unique constraint in the same transaction as the posting (posting identity, §11.4).

Source: PRD-001 §12.11

#### Scenario: Duplicate run cannot post twice
- **GIVEN** a handler has posted for an outbox row
- **WHEN** a duplicate run of the same row tries to post
- **THEN** the unique constraint on the business key rejects it, so the posting is not made twice

### Requirement: Outbox after a database restore
After a database restore, every outbox row that was not done at the restore point SHALL be set to unknown-outcome and reconciled before any dispatch.

Source: PRD-001 §12.11

#### Scenario: Not-done rows reconciled before dispatch
- **WHEN** the database is restored
- **THEN** every row that was not done at the restore point is set to unknown-outcome
- **AND** it is reconciled before any dispatch

#### Scenario: Sweep does not re-dispatch restored rows
- **WHEN** the sweep runs after a restore
- **THEN** it never re-dispatches those rows automatically

### Requirement: One outbox contract for every adapter
The local adapter, a push dispatcher (§12.8, rev 0.23, superseding River), SHALL implement the same outbox contract, and the outbox conformance suite (NFR-PORT-04) SHALL test the contract, not an adapter.

Source: PRD-001 §12.11

#### Scenario: Conformance suite tests the contract
- **WHEN** the outbox conformance suite runs against the local push dispatcher, and against the Cloud Tasks adapter in staging
- **THEN** both pass the same contract tests

#### Scenario: River kept for self-hosted installations
- **WHEN** Fyniq runs self-hosted without Cloud Tasks
- **THEN** River is the queue adapter option
- **AND** locally the push dispatcher is the adapter (rev 0.23)

### Requirement: Scale to zero except the database
Everything except the database SHALL scale to zero when idle; the database, which Hydra shares, SHALL be the always-on cost floor.

Source: PRD-001 §12.11

#### Scenario: Idle platform
- **WHEN** nobody uses the platform
- **THEN** every service and job scales to zero
- **AND** the database stays on, since Cloud SQL has no idle auto-stop and a manual stop still charges for storage and the IP address

#### Scenario: Cold start after idle
- **WHEN** the first request arrives after idle
- **THEN** it pays a cold start of a small static Go binary, kept within NFR-PERF-06

#### Scenario: Open live-updates stream
- **WHEN** a live-updates stream (FR-UX-19) is open
- **THEN** it keeps an instance billed while it is open

### Requirement: Idempotent handlers and worker classes
Every handler SHALL be idempotent on the outbox ID, and each worker class SHALL be a queue plus a service or job with its own identity, keeping the separation of duties of §12.3.

Source: PRD-001 §12.11

#### Scenario: Handler run twice for one outbox ID
- **WHEN** a handler runs twice for the same outbox ID
- **THEN** the result is the same as running it once

#### Scenario: Worker class identity
- **WHEN** a worker class is defined
- **THEN** it has its own queue and its own service or job with its own identity

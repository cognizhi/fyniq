# Tasks

## 1. Outbox

- [x] 1.1 Create the outbox table (state, attempts, lease, business key, task name, job execution) with RLS, and write rows through the unit of work; test (req: Transactional outbox, Outbox row contract) rolls back a change and asserts no row and no task
- [ ] 1.2 Add the `TaskQueue` port with the River local adapter and the Cloud Tasks adapter, enqueuing after commit with the row ID as the task name; outbox conformance suite `// req: NFR-PORT-04` runs against both (Cloud Tasks in staging)
- [x] 1.3 Make handlers idempotent on the outbox ID with leases, and postings unique on the business key; test (req: Idempotent handlers and worker classes, Posting runs at most once) delivers a task twice and asserts one effect

## 2. Tick, jobs and heartbeat

- [ ] 2.1 Add the tick endpoint: enqueue due schedules, sweep undispatched and expired rows, run missed slots once and never overlap; test (req: Scheduler tick, Missed slots and overlapping runs) with the frozen clock (`// req: DEV-05`)
- [ ] 2.2 Run long work as jobs started by a task (`JobRunner` port); test (req: Long work as Cloud Run jobs) starts the nightly verification job from p0-audit-chain through the tick
- [ ] 2.3 Write a heartbeat per tick, verification and anchoring run, with an absence alert outside the request path; test (req: Heartbeat outside the request path) stops the tick locally and asserts the alert fires

## 3. Failure paths and isolation

- [ ] 3.1 Mark time-outs after sending as unknown-outcome and reconcile before retry; after a restore, mark unfinished rows unknown-outcome; tests (req: Unknown outcomes of outside calls, Outbox after a database restore)
- [ ] 3.2 Give the executor its own queue, service account and OIDC audience, and allow only the `api` account to create its tasks; test (req: Proposal executor queue isolation) sends a task with another class's token and expects 403
- [ ] 3.3 Confirm every service idles to zero in staging and only the database stays on (req: Scale to zero except the database); then `make check` and `make test`

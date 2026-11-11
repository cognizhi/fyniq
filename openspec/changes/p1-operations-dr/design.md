# Design

## Context

The survey of 11 Nov 2026 (`efe03cf4`, the hardening's last slice) found this state.

**What exists:**
- **The database** (`infra/gcp/modules/environment/database.tf`): regional HA in production (from the platform spec),
  PITR with 7 days of logs, 35 daily backups pinned to `asia-southeast1`, CMEK, pgAudit on DDL and roles, Query Insights.
- **The organisation policy** (`modules/org-policy`): Singapore only (`gcp.resourceLocations`).
- **The security log sink** (`modules/security-logs`, p1-security-hardening): data-access logs for Cloud SQL, KMS,
  Storage, Secret Manager and IAM, and a 400-day bucket in `fyniq-audit`, locked in production.
- **The audit anchors' bucket**, locked (`modules/anchors`).
- **The jobs' commands:** `audit-anchor`, `audit-verify`, `audit-recover`, `heartbeat-check` (the database's view of the
  heartbeats) and `outbox-after-restore`. The platform job `ledger.verify` runs nightly; `docs/runbooks/audit-restore.md`
  covers a restore behind the anchors.
- **Evidence:** the GCS adapter sets Object Retention Lock and a temporary hold per object; `FYNIQ_GCP.evidence` names
  the staging and evidence buckets, which `infra/` does not create yet.
- **Operations:** `getOperation` polls one long operation (report exports, check runs); there is no list.
- **Logs:** request IDs in the API's log lines; no tracing (OpenTelemetry is only an indirect dependency).
- **Playwright** runs the smoke test in installed Chrome only.
- **The budget module** alerts at 50%, 80% and 100%.

**What is missing:** see proposal.md, Why.

## Goals / Non-Goals

**Goals**
- Every requirement of the change built and tested offline; the cloud parts as OpenTofu with module tests on mock
  providers, tagged `[live-verify]`, which `p1-launch-readiness` lists.
- Each drill and check leaves evidence the Compliance Center reads: a control run, a review, or a CI artefact.

**Non-Goals**
- DR-05 (a Backup and DR vault) and NFR-SCALE-02 (P3).
- A copy outside Singapore (DR-04, open question 37).
- Deploying staging and production (`cloud-first-deployment`).
- Paging services beyond Cloud Monitoring's channels.

## Decisions

### D1. The weekly export and the monthly backups (DR-02, DR-04, NFR-DR-01)

- **A module, `db-export`.** Cloud Scheduler calls Cloud SQL's export API each Sunday at 03:00 SGT, as its own
  account, which may only export (the pattern of `nonprod-scan`). It writes `gs://<audit bucket>/weekly/<date>.sql.gz`
  into a bucket in `fyniq-audit` (the anchors' project in production; staging's own):
  - under its own key, versioned;
  - with a 35-day retention policy, locked in production;
  - and a lifecycle that deletes weekly exports after 35 days.
- **The monthly copy.** The first export of each month is also written to `monthly/` and kept a year (DR-02's monthly
  backups). Exporting twice costs little; one bucket and one lock policy cover both. A second Scheduler job on the first
  Sunday does it.
- **What it covers.** Loss of `fyniq-prod`: RPO up to 7 days. Point-in-time recovery covers 5 minutes; regional HA
  covers zone loss.
- **Shredding.** The register the PDPA work built is re-applied on restore (`audit-recover`, §11.8), so an export holds
  nothing that outlives a shredding.
- *Alternatives:* on-demand Cloud SQL backups each month, which die with the project; Backup and DR (DR-05, P3).

### D2. Restore drills (DR-06, NFR-DR-02, NFR-DR-03, CTL-RES-02; rev 0.249)

- **A port, `ports.DatabaseClone`.** It clones the database at a time into a throwaway copy and gives a connection
  string, then removes it.
  - The GCP adapter uses Cloud SQL's `instances.clone` with a `pointInTime`, an instance with no public IP in the
    platform's network, and `instances.delete` [live-verify].
  - The local adapter uses `CREATE DATABASE … TEMPLATE fyniq` after the stack's connections are refused, and
    `DROP DATABASE`.
- **The monthly job, `restore.drill`.** A platform job, monthly, on the jobs' account. It:
  - clones the database at an hour ago;
  - runs the ledger verification and the audit-chain verification against the clone, as the worker's role, through
    the same code as the nightly jobs;
  - times the drill against NFR-DR-02's 4 hours;
  - records a control run of CTL-RES-02 (M) in the installation's organisation, a platform verification as
    `posture.findings` is;
  - removes the clone, whatever the result.
- **Locally,** `make restore-drill` does the same against the local database.
- **The quarterly drill.** `docs/runbooks/restore-drill.md`: restore the latest weekly export into the drill project,
  start the stack there, and check documents, keys, sign-in, the tick and the outbox's reconciliation
  (`outbox-after-restore`), timed against NFR-DR-01 and NFR-DR-02.
  - Its sign-off is a review (`restore-drill`, quarterly) in the Compliance Center, as the breach tabletop is: CTL-RES-02
    (Q) becomes an attestation.
- **Audit:** the drill job records `restore.drilled` in the installation's trail, with the clone's time, the verification
  results and the duration.
- *Alternatives:* both drills in a drill project, and the quarterly drill alone (the owner's decision).

### D3. Records and attachment storage (DR-07, NFR-RET-01, CTL-AUD-03)

- **`modules/evidence`.** It creates two buckets in the environment's project (Singapore, uniform access, public access
  prevented, CMEK) and their grants, and fills `FYNIQ_GCP.evidence`:
  - **staging**, unlocked, with a 7-day lifecycle on what was never committed;
  - **evidence**, versioned, with Object Retention Lock enabled, so each object carries its own retain-until in Locked
    mode (FR-DOC-02), set by the API at commit as today. No bucket-wide policy: retention differs per record (7 years
    from its financial year's end, legal holds).
- **A verification, `records.retention`, daily** (CTL-AUD-03's second check). For each file committed since the last
  run, through `ports.BlobStore`'s retention read, which the GCS and local adapters already have, it checks:
  - the object is in Locked mode;
  - its retain-until is at least the expiry its record computes (§11.9);
  - it has its record, and every committed record has its object (no orphans).
- *Alternatives:* a bucket-wide retention policy, which cannot follow per-record expiries; checking only the bucket
  setting, which misses an object committed without its lock.

### D4. Database and platform logs (CTL-AUD-04; §11.5)

- **The filter.** The security-logs sink adds Cloud SQL's pgAudit entries:
  `resource.type="cloudsql_database" AND logName:"postgres.log" AND textPayload:"AUDIT:"` [live-verify the log name].
- **CTL-AUD-04's check** becomes `tofu security-logs/production_keeps_its_security_logs_in_the_audit_project`, whose
  test gains the filter.
- **The "daily config check"** is what `infra.yml` drift detection is (CTL-CHG-02); the control says so.

### D5. Observability (NFR-OBS-01, NFR-AVAIL-01, CTL-RES-03; rev 0.249)

- **`internal/telemetry`.** It sets up OpenTelemetry once per binary, in `app.Serve`:
  - a tracer provider with a parent-based ratio sampler (10%), plus a tail rule that exports every span of a request
    that answered 5xx;
  - W3C trace context propagated through `httpserver`'s middleware and every egress client (an `otelhttp` transport
    around the guard's);
  - the span attributes `fyniq.organisation`, `fyniq.request_id`, `fyniq.client` and `http.route`;
  - the log handler adds `trace_id` and `span_id`, so Cloud Logging links each line to its trace.
- **Exporters:**
  - **release:** the Google Cloud trace exporter, in `internal/adapters/gcp` (ADR-T8: the SDK stays in the adapter);
  - **local:** OTLP over HTTP when `FYNIQ_OTLP_ENDPOINT` is set, otherwise none.
- **Metrics** are Cloud Run's request count and latency. Fyniq adds no metrics exporter now.
- **`modules/slo`.** Per public service (api, mcp, oauth):
  - an availability SLO of 99.9% monthly (NFR-AVAIL-01), and a latency SLO (95% of requests under 300 ms, NFR-PERF-01),
    from Cloud Run's metrics;
  - burn-rate alerts: fast (14.4× over 1 hour) and slow (6× over 6 hours), to the founders' channels;
  - a dashboard [live-verify]. CTL-RES-03 becomes `tofu slo/…`.
- *Alternatives:* logs and metrics only, and every request traced (the owner's decision).

### D6. The heartbeat (NFR-OBS-02, CTL-RES-05)

- **The log line.** Each `Beat` (the tick, `ledger.verify`, `audit.anchor`, `audit.verify`) also logs a structured
  `fyniq.heartbeat` entry with the job's name. The services log to Cloud Logging whatever the database's state, so the
  alert does not depend on the database, the API or the Scheduler.
- **`modules/heartbeat`.** A log-based metric per heartbeat, and Cloud Monitoring metric-absence alerts:
  - the tick: 30 minutes, to the platform owner's channel; a second policy at 60 minutes, to the Owner's (escalation by
    a longer window, since Cloud Monitoring has no acknowledgement-based escalation);
  - each daily job: its schedule plus 2 hours, if the absence window allows it [verify the alerting limits: Cloud
    Monitoring caps metric-absence durations]; otherwise a log-match alert on the nightly `heartbeat-check` job's
    failure.
- **The daily configuration check** (CTL-RES-05 D, second check). A platform verification, `scheduler.configured`,
  reads through a port (`ports.Scheduling`, GCP: Cloud Scheduler and Monitoring's APIs; local: a fake) and checks:
  - the tick and the platform jobs' Scheduler jobs exist and are enabled;
  - the absence policies exist, are enabled and have a channel.
- **The quarterly drill** (CTL-RES-05 Q): disabling the tick in staging raises the alert. It is a review,
  `heartbeat-drill`, signed off in the Compliance Center, with the runbook's steps.

### D7. Performance (NFR-PERF-01 to NFR-PERF-06, NFR-SCALE-01, QA-PERF-01; rev 0.249)

- **`cmd/perf`** (a tool built with `-tags local`, like `cmd/seed`):
  - it grows a synthetic organisation to N ledger lines, through the posting engine in batches, from the sample
    generator's shapes (so every invariant holds);
  - it times each budget through the API of the running local stack: reads (p95 ≤ 300 ms), the standard reports for 12
    months with comparatives (p95 ≤ 2 s at 1 M lines, ≤ 5 s at 10 M), posting a 50-line document (p95 ≤ 500 ms), a
    10,000-line bank import (≤ 60 s), a 1 M-line GL export (≤ 5 min), and the first request after the API restarts
    (NFR-PERF-06, reported, not gated);
  - it writes `.data/perf/report.json` and `report.md`, and fails a gated budget it misses.
- **`make perf`** (`LINES=1000000` by default).
- **`perf.yml`:**
  - weekly at 1 M lines, the report as an artefact;
  - on the release pull request, at 10 M, attached to the release as evidence (`release-assets.json`);
  - QA-PERF-01's control reads it.
- **NFR-SCALE-01's 100 concurrent users:** the harness runs the read budget with 100 concurrent sessions.

### D8. Browsers (NFR-BROWSER-01)

- Playwright gains `firefox` and `webkit` projects beside Chrome; CI installs the browsers (`npx playwright install
  --with-deps`).
- Vite's `build.target` names the supported browsers' oldest versions, from a browserslist query of the last two
  versions [verify the versions at build]. A test keeps the two in step.

### D9. Cost (NFR-COST-01 to NFR-COST-04; rev 0.249)

- **The idle alert (NFR-COST-02),** a policy in `modules/slo`. It fires when Cloud Run's billable instance time is above
  zero for an hour in which the services served no request, the scanner's warm instance excepted [live-verify].
- **NFR-COST-04's reconciliation:**
  - a review, `cost-model`, before go-live: the first staging month's bill against §12.6, which is updated from it;
  - a template in `docs/runbooks/cost-review.md`.
- **NFR-COST-01 and NFR-COST-03** are the budget module's (built), now listed as built.

### D10. The operations tray (screen `operations-tray`; FR-INT-11, NFR-PERF-04, NFR-PERF-05)

- **`listMyOperations`** (`GET /v1/operations`): the caller's operations, newest first, with cursor paging. Each has
  its kind, its status (from its job's state), when it started and finished, and its resource. Only the caller's own,
  as `getOperation` (no new Appendix D row: Operations).
- **In the web app,** a header button with the count running. It opens a panel that:
  - lists them, polling every 5 seconds while any runs;
  - shows a running one's progress as indeterminate (operations carry no percentage yet);
  - links a finished one to its result, such as the export's download;
  - shows a failed one's reason.
  The notification of a finished export exists (FR-NTF-01).
- **Help keys** `shell.operations`; a guide section; a component test with axe; the fragment `OperationsTray`,
  local only until the design account is available.

## Risks / Trade-offs

- **[Most of it runs only in the cloud]** Clones, exports, SLOs, absence alerts and Cloud Trace are verified in the dev
  cloud. Mitigation: ports with local adapters for the drill and the configuration check; module tests; `[live-verify]`.
- **[A clone costs while it lives]** Mitigation: removed in the same job, whatever the result; the job's timeout is 4
  hours; an orphan clone shows in the next run's check.
- **[The local harness is not Cloud SQL]** Its numbers are a laptop's. Mitigation: trends weekly; staging's numbers at
  the cloud gate [live-verify]; the budgets gate only the 10 M release run.
- **[Absence alerts have limits]** Daily jobs may exceed Cloud Monitoring's absence window. Mitigation: the nightly
  `heartbeat-check` job's failure as a log-match alert.
- **[Sampling hides a slow request]** Mitigation: every error kept; the latency SLO from Cloud Run's metrics counts
  every request.

## Migration Plan

- Each slice ships as its own pull request, stacked, with its gates. No migration is expected; `listMyOperations`
  reads the existing table.
- The evidence buckets' creation changes `FYNIQ_GCP.evidence` from unset to set: uploads start working in the cloud.

## Open Questions

- The alerting limits of metric-absence policies for daily jobs [verify at the cloud gate]; D6 has the fallback.

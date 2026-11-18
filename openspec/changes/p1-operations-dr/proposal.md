# Proposal

## Why

Fyniq goes live holding the company's books, and nothing yet proves it can be restored or that anyone would notice
it failing. P0 configured the database for HA, PITR and 35 days of backups, but:
- the weekly export the DR posture relies on (DR-04) does not exist;
- the monthly backups kept a year (DR-02) do not exist;
- no restore has ever been drilled (DR-06, CTL-RES-02);
- the evidence buckets the API writes to are not in `infra/` (DR-07, CTL-AUD-03);
- there are no traces, SLOs or burn-rate alerts (NFR-OBS-01, CTL-RES-03);
- the Scheduler heartbeat has no alert independent of the app (NFR-OBS-02, CTL-RES-05);
- the performance budgets have never been measured (NFR-PERF, QA-PERF-01);
- only Chrome is tested (NFR-BROWSER-01);
- nothing alerts on compute billed while idle (NFR-COST-02).

Five control rows of Appendix F wait for this change: CTL-AUD-03 (the records bucket), CTL-AUD-04, CTL-RES-02,
CTL-RES-03 and CTL-RES-05 (the alert and its drill). The launch plan puts it in wave 7, before go-live. Its cloud parts
are built as code with module tests and verified in the dev cloud later.

## What Changes

- **The owner's decisions of 11 Nov 2026, taken interactively (PRD-001 rev 0.249):**
  - **Restore drills (CTL-RES-02, DR-06).** Each month a job clones the production database in production's own
    project (Cloud SQL clones only within a project), where no app can reach it. It verifies the ledger and the audit
    chain on the clone, records the evidence and deletes the clone. Each quarter the full drill restores the weekly
    export into an isolated drill project, by runbook, timed against NFR-DR-02.
    - *Not chosen:* both in a drill project, and the quarterly drill alone.
  - **Traces (NFR-OBS-01).** OpenTelemetry with W3C trace context across the services, exported to Cloud Trace
    through an adapter: 10% sampled, every error kept, none locally unless an OTLP endpoint is set. The SLOs read Cloud
    Run's request metrics.
    - *Not chosen:* logs and metrics only, and every request traced.
  - **Performance (QA-PERF-01).** A harness on the local stack, weekly in CI at 1 M lines, and at 10 M before each
    minor release with its report as evidence; staging's numbers are checked in the cloud [live-verify].
    - *Not chosen:* 1 M on every pull request, and staging only.
  - **Idle cost (NFR-COST-02).** The scanner service's one warm instance in production joins the fixed fees billed
    while idle, with its cost in §12.6 [verify].
    - *Not chosen:* scaling it to zero, and warm in business hours.
- **Backups and the DR copy (DR-01 to DR-04, NFR-DR-01):**
  - the weekly encrypted export of the database into a retention-locked bucket in `fyniq-audit`, kept 35 days, the
    first of each month kept a year (DR-02's monthly backups);
  - the HA, PITR, backup and location settings asserted by module tests.
- **Restore drills (DR-06, NFR-DR-02, NFR-DR-03, CTL-RES-02):**
  - a database-clone port, with a Cloud SQL adapter and a local one (a template database);
  - the monthly drill job, and `make restore-drill` locally;
  - the quarterly drill as a runbook and a signed-off review in the Compliance Center.
- **Records and attachments (DR-07, NFR-RET-01, CTL-AUD-03):**
  - the staging and evidence buckets in `infra/`: Singapore, versioned, the evidence bucket with Object Retention Lock;
  - a daily verification that every committed file is locked until its record's retention ends, and that no file is
    without its record.
- **Logs (CTL-AUD-04; §11.5's database and platform logs):** pgAudit's entries join the security log sink. CTL-AUD-04's
  check becomes the security-logs module test, which `p1-security-hardening` built.
- **Observability (NFR-OBS-01, NFR-AVAIL-01, CTL-RES-03):**
  - tracing in the services, with the tenant and request IDs on every span and log line;
  - SLOs for availability (99.9%) and latency, with burn-rate alerts and a dashboard [live-verify].
- **The heartbeat (NFR-OBS-02, CTL-RES-05):**
  - Cloud Monitoring absence alerts on the tick's and the daily jobs' heartbeats, routed to the platform owner and,
    after a second window, the Owner [verify the alerting limits];
  - a daily check that the alert, its channel and the Scheduler jobs exist and are enabled;
  - the quarterly drill of disabling the tick in staging, as a review.
- **Performance and scale (NFR-PERF-01 to NFR-PERF-06, NFR-SCALE-01):** `make perf` measures the budgets on a synthetic
  tenant, with the weekly and release workflows above.
- **Browsers (NFR-BROWSER-01):** the web smoke test runs in Chromium, Firefox and WebKit, and the build targets the
  supported browsers.
- **Cost (NFR-COST-01 to NFR-COST-04):**
  - an alert when service compute is billed while idle;
  - the cost model's reconciliation with the first staging month, as a review [live-verify].
- **The operations tray (screen `operations-tray`; FR-INT-11, NFR-PERF-04, NFR-PERF-05):**
  - `listMyOperations`, the caller's long operations;
  - a panel from the header that follows them to their result;
  - help keys, the guide, a component test with axe, and its fragment, local only.

## Capabilities

### New Capabilities

None: the requirements are approved in the main specs, and rev 0.249's changes are made there directly.

### Modified Capabilities

None as deltas: `skip_specs: true`. Rev 0.249 edits these main specs, stamped against the PRD:
- NFR-OBS-01 in `platform/operations`;
- NFR-COST-02 in `platform/portability-and-cost`;
- QA-PERF-01 in `quality/test-gates`;
- CTL-RES-02 in `compliance/technology-controls`.

## Impact

- **Requirements:**
  - `data/backup-and-recovery`: DR-01, DR-02, DR-03, DR-04, DR-06, DR-07;
  - `platform/operations`: NFR-DR-01, NFR-DR-02, NFR-DR-03, NFR-RET-01, NFR-OBS-01, NFR-OBS-02;
  - `platform/performance-and-scale`: NFR-PERF-01 to NFR-PERF-06, NFR-SCALE-01, NFR-AVAIL-01, NFR-BROWSER-01;
  - `platform/portability-and-cost`: NFR-COST-01 to NFR-COST-04;
  - `compliance/financial-controls`: CTL-AUD-03, CTL-AUD-04;
  - `compliance/technology-controls`: CTL-RES-02, CTL-RES-03, CTL-RES-05;
  - `data/audit-trail`: the database and platform logs layer.
- **Screens:** `operations-tray` (new to the build; designed locally, published when the design account is available).
- **Code:**
  - a database-clone port and the drill job;
  - the records-retention verification;
  - `internal/telemetry` (OpenTelemetry) with a GCP and a local exporter;
  - the heartbeat configuration verification;
  - `cmd/perf` (or `test/perf`) and `make perf`;
  - `listMyOperations` and the web tray;
  - Playwright projects.
- **Infrastructure:** `infra/gcp/modules/`:
  - the database export and its locked bucket;
  - the evidence buckets;
  - the SLOs, burn-rate and heartbeat alerts;
  - the idle-cost alert;
  - the drill job's grants;
  - each with module tests [live-verify].
- **Database:** none expected; the operations list reads the existing `operation` table.
- **API:** additive: `listMyOperations`.
- **Not here:**
  - DR-05 (the Backup and DR vault) and NFR-SCALE-02 (P3);
  - the copy outside Singapore, which waits for a transfer assessment (DR-04, open question 37);
  - the deployment of staging and production, which `cloud-first-deployment` makes.

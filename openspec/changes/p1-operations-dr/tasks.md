# Tasks

Each slice is its own pull request, stacked on the one before, with its gates (`make check`, `make test`'s suites
for what it touches, `make api-check`, archtest; `make infra-check` for `infra/`). Cloud parts are built as code with
module tests on mock providers and tagged `[live-verify]`.

## 1. The plan (slice 1)

- [x] 1.1 PRD-001 rev 0.249 records the owner's decisions of 11 Nov 2026 (CTL-RES-02, NFR-OBS-01, QA-PERF-01,
  NFR-COST-02, and §12.6's cost). The specs are updated and stamped, and CTL-RES-02's file is synced.
- [x] 1.2 This change: the proposal, the design (D1–D10) and these tasks. The build plan's paragraph says it is
  proposed.

## 2. Backups and the DR copy (slice 2)

- [x] 2.1 `infra/gcp/modules/db-export` (D1): the weekly export as its own export-only account into a CMEK bucket in the
  audit project (35 days, locked in production), and the first Sunday's monthly copy kept a year, with a module test
  `[live-verify]`; the stack wires it. `// req: DR-02`, `// req: DR-04`, `// req: NFR-DR-01`
  - As built: the platform job `db.export` runs it as the jobs' account, with a custom role that may only export and
    read the instance; the month's copy goes into its own bucket kept a year (design D1).
- [x] 2.2 The environment's module test asserts regional HA in production, PITR with 7 days of logs, 35 daily backups
  and their location in Singapore. `// req: DR-01`, `// req: DR-02`, `// req: DR-03`

## 3. Records and attachment storage (slice 3)

- [ ] 3.1 `infra/gcp/modules/evidence` (D3): the staging and evidence buckets (Singapore, CMEK, versioned, Object
  Retention Lock on the evidence bucket), their grants and `FYNIQ_GCP.evidence`, with a module test `[live-verify]`.
  `// req: DR-07`, `// req: NFR-RET-01`
- [ ] 3.2 The `records.retention` verification, daily: each committed file locked until its record's expiry, none
  without its record and none missing, with tests on the local store. CTL-AUD-03's second check becomes `running`.
  `// req: CTL-AUD-03`, `// req: NFR-RET-01`

## 4. Restore drills (slice 4)

- [ ] 4.1 `ports.DatabaseClone` with the Cloud SQL adapter (clone at a time, delete; httptest) and the local one (a
  template database). `// req: DR-06`
- [ ] 4.2 The `restore.drill` platform job, monthly (D2): clone an hour back, verify the ledger and the audit chain on the
  clone, time it, record `restore.drilled` and CTL-RES-02's monthly run, remove the clone whatever happens; `make
  restore-drill` locally, with a database test. `// req: DR-06`, `// req: NFR-DR-02`, `// req: NFR-DR-03`,
  `// req: CTL-RES-02`
- [ ] 4.3 `docs/runbooks/restore-drill.md` and the `restore-drill` review (quarterly) in the Compliance Center; CTL-RES-02's
  quarterly check becomes an attestation; the jobs' grants in `infra/` (clone and delete on the instance only)
  `[live-verify]`. `// req: DR-06`, `// req: CTL-RES-02`

## 5. Database and platform logs (slice 5)

- [ ] 5.1 The security-logs sink gains pgAudit's entries (D4), with its module test `[live-verify]`; CTL-AUD-04's check
  becomes `cloud` on that test. `// req: CTL-AUD-04`, and the audit trail's database and platform logs layer

## 6. Observability (slice 6)

- [ ] 6.1 `internal/telemetry` (D5): the tracer provider in `app.Serve`, 10% sampled with every failed request kept,
  W3C context through `httpserver` and every egress client, the tenant and request IDs on spans and log lines; the GCP
  exporter in `internal/adapters/gcp` and the local OTLP one, with tests. `// req: NFR-OBS-01`
- [ ] 6.2 `infra/gcp/modules/slo`: availability (99.9%) and latency SLOs for the public services, fast and slow
  burn-rate alerts and a dashboard, with a module test `[live-verify]`; CTL-RES-03 becomes `cloud`.
  `// req: NFR-OBS-01`, `// req: NFR-AVAIL-01`, `// req: CTL-RES-03`

## 7. The heartbeat (slice 7)

- [ ] 7.1 Every `Beat` logs `fyniq.heartbeat` with its job (D6); `infra/gcp/modules/heartbeat`: the log-based metrics and
  the absence alerts (the tick at 30 minutes to the platform owner, 60 to the Owner; the daily jobs, or the
  `heartbeat-check` fallback) `[verify the alerting limits]`, with a module test. `// req: NFR-OBS-02`
- [ ] 7.2 `ports.Scheduling` (GCP and fake) and the `scheduler.configured` verification, daily; the `heartbeat-drill`
  review, quarterly; CTL-RES-05's two planned checks become `running` and an attestation. `// req: NFR-OBS-02`,
  `// req: CTL-RES-05`

## 8. Performance and scale (slice 8)

- [ ] 8.1 `cmd/perf` and `make perf` (D7): the synthetic organisation grown through the engine, every budget timed
  through the local stack's API, 100 concurrent readers, the report in `.data/perf/`. `// req: NFR-PERF-01` to
  `// req: NFR-PERF-06`, `// req: NFR-SCALE-01`
- [ ] 8.2 `.github/workflows/perf.yml`: weekly at 1 M lines, and at 10 M on the release pull request with the report
  attached to the release (`release-assets.json`); QA-PERF-01's evidence. `// req: QA-PERF-01`

## 9. Browsers (slice 9)

- [ ] 9.1 Playwright's Firefox and WebKit projects in CI, and Vite's build target from the supported browsers, with a
  test that keeps them in step. `// req: NFR-BROWSER-01`

## 10. Cost (slice 10)

- [ ] 10.1 The idle-compute alert (D9) in `infra/`, with a module test `[live-verify]`; `docs/runbooks/cost-review.md`
  and the `cost-model` review before go-live; NFR-COST-01 and NFR-COST-03 recorded as the budget module's.
  `// req: NFR-COST-01`, `// req: NFR-COST-02`, `// req: NFR-COST-03`, `// req: NFR-COST-04`

## 11. The operations tray (slice 11)

- [ ] 11.1 `listMyOperations` (`GET /v1/operations`, cursor paging, the caller's own), with its matrix rows and tests.
  `// req: FR-INT-11`
- [ ] 11.2 The header's operations button and panel (D10), polling while any runs, linking each result; help key
  `shell.operations`, the guide section, a component test with axe, and the `OperationsTray` fragment (local; the
  canvas waits for the design account). `// req: FR-INT-11`, `// req: NFR-PERF-04`, `// req: NFR-PERF-05`

## 12. Threat model and as built (slice 12)

- [ ] 12.1 `docs/security/threat-models/operations-dr.md`.
- [ ] 12.2 `make test` and `make up` pass on the last slice; the build plan's paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-operations-dr`, then `/opsx:archive p1-operations-dr --skip-specs` once every task is ticked.
- `p1-launch-readiness` lists every `[live-verify]` of this change for the dev cloud.

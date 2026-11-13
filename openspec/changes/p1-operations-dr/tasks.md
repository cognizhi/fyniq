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

- [x] 3.1 `infra/gcp/modules/evidence` (D3): the staging and evidence buckets (Singapore, CMEK, versioned, Object
  Retention Lock on the evidence bucket), their grants and `FYNIQ_GCP.evidence`, with a module test `[live-verify]`.
  `// req: DR-07`, `// req: NFR-RET-01`
- [x] 3.2 The `records.retention` verification, daily: each committed file locked until its record's expiry, none
  without its record and none missing, with tests on the local store. CTL-AUD-03's second check becomes `running`.
  `// req: CTL-AUD-03`, `// req: NFR-RET-01`
  - As built: `ports.BlobStore.RetentionOf` reads an object's retention; staging has no age-based deletion of uploads
    (a draft's file waits there), only a 7-day backstop on report exports (design D3).

## 4. Restore drills (slice 4)

- [x] 4.1 `ports.DatabaseClone` with the Cloud SQL adapter (clone at a time, delete; httptest) and the local one (a
  template database). `// req: DR-06`
- [x] 4.2 The `restore.drill` platform job, monthly (D2): clone an hour back, verify the ledger and the audit chain on the
  clone, time it, record `restore.drilled` and CTL-RES-02's monthly run, remove the clone whatever happens; `make
  restore-drill` locally, with a database test. `// req: DR-06`, `// req: NFR-DR-02`, `// req: NFR-DR-03`,
  `// req: CTL-RES-02`
- [x] 4.3 `docs/runbooks/restore-drill.md` and the `restore-drill` review (quarterly) in the Compliance Center; CTL-RES-02's
  quarterly check becomes an attestation; the jobs' grants in `infra/` (clone and delete on the instance only)
  `[live-verify]`. `// req: DR-06`, `// req: CTL-RES-02`
  - Slice 4a built 4.1, 4.2 (CTL-RES-02's monthly run is the check `job restore.drill`, the job's own outcome; design
    D2) and 4.3's grants (`infra/gcp/modules/environment/database.tf`); slice 4b the runbook, the restore drill register
    (migration 000317, `listRestoreDrills`, `recordRestoreDrill`, Compliance › Controls › Restore drills; the DPO
    records, as Appendix D's Compliance Center allows) and the review `restore-drill`, in the installation's
    organisation only.

## 5. Database and platform logs (slice 5)

- [x] 5.1 The security-logs sink gains pgAudit's entries (D4), with its module test `[live-verify]`; CTL-AUD-04's check
  becomes `cloud` on that test. `// req: CTL-AUD-04`, and the audit trail's database and platform logs layer

## 6. Observability (slice 6)

- [x] 6.1 `internal/telemetry` (D5): the tracer provider in `app.Serve`, 10% sampled with every failed request kept,
  W3C context through `httpserver` and every egress client, the tenant and request IDs on spans and log lines; the GCP
  exporter in `internal/adapters/gcp` and the local OTLP one, with tests. `// req: NFR-OBS-01`
- [x] 6.2 `infra/gcp/modules/slo`: availability (99.9%) and latency SLOs for the public services, fast and slow
  burn-rate alerts and a dashboard, with a module test `[live-verify]`; CTL-RES-03 becomes `cloud`.
  `// req: NFR-OBS-01`, `// req: NFR-AVAIL-01`, `// req: CTL-RES-03`

## 7. The heartbeat (slice 7)

- [x] 7.1 Every `Beat` logs `fyniq.heartbeat` with its job (D6); `infra/gcp/modules/heartbeat`: the log-based metrics and
  the absence alerts (the tick at 30 minutes to the platform owner, 60 to the Owner; the daily jobs, or the
  `heartbeat-check` fallback) `[verify the alerting limits]`, with a module test. `// req: NFR-OBS-02`
- [x] 7.2 `ports.Scheduling` (GCP and fake) and the `scheduler.configured` verification, daily; the `heartbeat-drill`
  review, quarterly; CTL-RES-05's two planned checks become `running` and an attestation. `// req: NFR-OBS-02`,
  `// req: CTL-RES-05`
  - Slice 7a built the port, its adapters and `scheduler.configured` (CTL-RES-05's daily check is running); slice 7b the
    heartbeat drill register (migration 000318, `fyniq.tick_drill`), `listHeartbeatDrills` and `recordHeartbeatDrill`,
    the Heartbeat drills tab, `docs/runbooks/heartbeat-drill.md` and the review `heartbeat-drill` (CTL-RES-05 Q).

## 8. Performance and scale (slice 8)

- [x] 8.1 `cmd/perf` and `make perf` (D7): the synthetic organisation grown through the engine, every budget timed
  through the local stack's API, 100 concurrent readers, the report in `.data/perf/`. `// req: NFR-PERF-01` to
  `// req: NFR-PERF-06`, `// req: NFR-SCALE-01`
- [x] 8.2 `.github/workflows/perf.yml`: weekly at 1 M lines, and at 10 M on the release pull request with the report
  attached to the release (`release-assets.json`); QA-PERF-01's evidence. `// req: QA-PERF-01`
  - As built: the 10 M run is on the release commit (release-please's `chore(release)` pushed to main), not the release
    pull request, because the release's evidence is read from the release commit's push runs; the evidence job waits
    up to 5¾ hours `[verify a 10 M run fits GitHub's six]`.

## 9. Browsers (slice 9)

- [x] 9.1 Playwright's Firefox and WebKit projects in CI, and Vite's build target from the supported browsers, with a
  test that keeps them in step. `// req: NFR-BROWSER-01`

## 10. Cost (slice 10)

- [x] 10.1 The idle-compute alert (D9) in `infra/`, with a module test `[live-verify]`; `docs/runbooks/cost-review.md`
  and the `cost-model` review before go-live; NFR-COST-01 and NFR-COST-03 recorded as the budget module's.
  `// req: NFR-COST-01`, `// req: NFR-COST-02`, `// req: NFR-COST-03`, `// req: NFR-COST-04`

## 11. The operations tray (slice 11)

- [x] 11.1 `listMyOperations` (`GET /v1/operations`, cursor paging, the caller's own), with its matrix rows and tests.
  `// req: FR-INT-11` Building added the index `operation_requested` (migration 000319, built concurrently), so the list
  reads only the caller's rows.
- [x] 11.2 The header's operations button and panel (D10), polling while any runs, linking each result; help key
  `shell.operations`, the guide section, a component test with axe, and the `OperationsTray` fragment (local; the
  canvas waits for the design account). `// req: FR-INT-11`, `// req: NFR-PERF-04`, `// req: NFR-PERF-05` Building added
  the kit's `fy-progress--indeterminate` (the skeleton's sheen when motion is on, still when off). The button shows from
  the medium class up: the compact header had 18 px left at 390 px, and `p1-app-shell` lays it out again.

## 12. Threat model and as built (slice 12)

- [x] 12.1 `docs/security/threat-models/operations-dr.md`.
- [x] 12.2 `make test` and `make up` pass on the last slice; the build plan's paragraph says what was built.

## 13. The owner's decisions after building (rev 0.250, design D11)

- [x] 13.1 PRD-001 rev 0.250 and the specs it changes (DR-04, DR-07, SEC-11, NFR-OBS-01, NFR-PERF-05, QA-PERF-01,
  FR-INT-09).
- [x] 13.2 The evidence buckets and the key ring of `fields` and `fingerprints` in `fyniq-audit`, granted to the api and
  the worker alone, with module tests `[live-verify]`. `// req: DR-07`, `// req: SEC-11`, `// req: DR-04` Building added
  the module `field-keys`; the environment module takes the keys as an input.
- [x] 13.3 The environment's `traces` setting, off until Cloud Trace keeps spans in Singapore; off, the services export
  nothing. Module test and the runbook's check `[live-verify]`. `// req: NFR-OBS-01`, `// req: CTL-DP-07` The check is
  in `infra/README.md`.
- [x] 13.4 The general ledger export streamed a page at a time with no limit, its figures checked against the
  on-screen report and the oracle's, and the harness's NFR-PERF-05 at 1 M lines. `// req: NFR-PERF-05` Built: a keyset
  read of an account's lines (`AccountLinesAfter`), `balance.Opening` and `Walk`, streaming CSV and XLSX writers (a
  sheet goes on in another at Excel's row limit), the worker piping the file into the store, and the GCS adapter's
  resumable upload in 8 MiB chunks. The on-screen general ledger is the reference the export is checked against: the
  oracle's general ledger source compares postings and trial balances, not the report's running balances. Measured
  locally at 200,000 lines: 200,158 rows in 1m26s; the 1 M run is the weekly job's.
- [ ] 13.5 The reports measured at 1 M lines and made to meet NFR-PERF-02. `// req: NFR-PERF-02`
- [x] 13.6 `If-Match` accepts the API's own tag weak or strong. `// req: FR-INT-09` The harness now sends the ETag back as it
  came. Building found Caddy names the encoding in the tag (`"1-draft-gzip"`), so that is accepted too (rev 0.251).
- [ ] 13.7 The threat model and the build plan say what was built.

## Workflow follow-up

- `/opsx:verify p1-operations-dr`, then `/opsx:archive p1-operations-dr --skip-specs` once every task is ticked.
- `p1-launch-readiness` lists every `[live-verify]` of this change for the dev cloud.

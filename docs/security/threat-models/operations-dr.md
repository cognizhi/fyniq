# Threat model: operations and disaster recovery

| | |
|---|---|
| Feature and requirement IDs | Backups, the DR copy and restore drills; the evidence buckets; pgAudit in the security sink; traces, SLOs and the heartbeat's alerts; the performance harness; the supported browsers; the idle-cost alert; the operations tray: DR-01 to DR-04, DR-06, DR-07, NFR-DR-01 to NFR-DR-03, NFR-RET-01, NFR-OBS-01, NFR-OBS-02, NFR-PERF-01 to NFR-PERF-06, NFR-SCALE-01, NFR-AVAIL-01, NFR-BROWSER-01, NFR-COST-01 to NFR-COST-04, FR-INT-11, CTL-AUD-03, CTL-AUD-04, CTL-RES-02, CTL-RES-03, CTL-RES-05; from task 13 (rev 0.250 to 0.252) FR-INT-09, FR-UX-02, FR-CTL-01, INV-08, INV-11, DR-07, SEC-11 (PRD-001; `p1-operations-dr`) |
| Author, reviewer, date | Claude Code for the owner, 11 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Every class the platform holds, in full: the weekly export, the monthly copy and the drill's clone are copies of the whole database. The evidence buckets hold documents and records (personal and confidential). Spans and log lines hold request metadata: the route's pattern, the status, the organisation's, the client's and the request's IDs. The drill registers' findings are free text and may name people. The performance harness's organisation is synthetic (DEV-08) |
| Who acts | The jobs' account (the export, the clone and its removal, the checks, the heartbeat check); Cloud SQL's service agent (writes the export); the api and the worker (commit evidence, send spans); the DPO (the drill registers); the Owner with the break-glass holder (the quarterly drill); the platform owner and the founders (alerts); CI (the harness on the local stack); attackers holding production's project, a service's identity, a member's session, or nothing but the internet |

## 1. What we are building

Each week the jobs' account asks Cloud SQL to export the database into a bucket in `fyniq-audit`, encrypted with a key
there and locked for 35 days; the first export of each month is copied to a second bucket kept 365 days. Cloud SQL's
service agent may only create objects in it, and nobody may read it until a drill or a recovery grants it, by runbook.
Production's database must be regional, with point-in-time recovery.

Each month the restore drill clones production's database as it was an hour ago, in production's own project and on
its private network. It opens the copy with the services' own credentials, runs the ledger's and the audit chain's
verifications on it, records what it found as an audit event, and removes the copy whatever the result. Each quarter a
person restores the weekly export into an isolated project by runbook, and the DPO records both kinds of drill in an
insert-only register.

The evidence buckets lock each committed file under its retention, and a daily check reads every committed file's
lock. pgAudit's entries join the security log sink in `fyniq-audit`. The services send OpenTelemetry spans to Cloud
Trace: a tenth of the requests, and every failed one, with only allow-listed attributes. SLOs over Cloud Run's own
request metrics page the founders on fast and slow burn. Every job logs a heartbeat; Cloud Monitoring alerts on its
absence, independently of the app, and an hourly check compares each job's slots in the database with its schedule.
The performance harness grows a synthetic ledger on the local stack and times the budgets through the API. The
operations tray lists the long work a person asked for.

Trust boundaries: the production project ↔ `fyniq-audit` (exports, keys, logs, anchors); the jobs ↔ Cloud SQL's
admin API (export, clone, remove); the jobs ↔ the clone (the database protocol on the private network); the services
↔ Cloud Trace and Cloud Logging (Google APIs); Cloud Monitoring ↔ people (email); browser ↔ API (the tray); CI ↔ the
local stack (the harness).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Exports bucket | Information disclosure: a copy of the whole database read by someone who should not, or made public | Every organisation's books and personal data | Medium |
| T2 | Production project | Tampering / denial: someone holding production (an attacker, ransomware or a mistake) deletes the database and its backups | The books lost | Low |
| T3 | Jobs → Cloud SQL admin | Elevation: the drill's rights to remove its copies reach the database itself, or its clone rights reach another instance | Production deleted | Low |
| T4 | The drill's clone | Information disclosure: a full copy of production, reachable for hours, or left behind | Every organisation's data in a second place | Medium |
| T5 | Drill registers | Repudiation / tampering: a drill recorded as passed that never ran or failed; a record changed after the fact | False assurance at audit | Low |
| T6 | The restored copy | Tampering: a restore that lost the newest entries, or a cut chain, passes as good | A restore believed complete that is not | Medium |
| T7 | Evidence buckets | Tampering / repudiation: a committed record or evidence file deleted, replaced or its retention shortened | Records the law requires lost (CTL-AUD-03) | Medium |
| T8 | Database sessions | Repudiation: a direct session, such as break-glass, leaves no trace outside the database | An unseen change | Low |
| T9 | Services → Cloud Trace | Information disclosure: a span carries personal data, a secret, a query string or a body | Data outside its controls, in a store the log scan does not read | Medium |
| T10 | Services → outside hosts | Information disclosure: the trace context sent to a connector's or a webhook's host | Our trace IDs and sampling known outside | Low |
| T11 | Internet → services → Cloud Trace | Denial (cost): a caller forces every span to be kept with a sampled `traceparent`, or by failing requests | Trace cost | Medium |
| T12 | Scheduler → jobs | Denial / repudiation: the tick stops, or is stopped, and nobody notices; the anchors and nightly verifications stop | Late detection of anything else | Medium |
| T13 | Services → Cloud Logging | Spoofing: a forged heartbeat entry hides a stopped tick | As T12 | Low |
| T14 | Services → people | Denial: an outage pages nobody, or pages so often the pages are ignored | Late recovery | Medium |
| T15 | CI → the harness | Information disclosure / tampering: the harness run against production, or with production data | DEV-08 broken; posted history changed | Low |
| T16 | Billing | Denial (cost): compute billed while nobody uses it, unnoticed | Cost | Medium |
| T17 | Browser → API (the tray) | Information disclosure: another person's operations, and the exports they name, listed | Someone else's exports discovered | Medium |
| T18 | Browser | Tampering: a browser too old for the platform's CSP, passkeys or the build's syntax | Weaker protections, broken pages | Low |
| T19 | Browser → API (search) | Tampering / denial: a search's text taken as a LIKE pattern, its wildcards widening the match, or a text that makes the database read every document | Wrong results; slow searches for everyone | Low |
| T20 | Reports ↔ checks | Tampering (integrity): a balances cache that drifted from its lines is read by the statements too, so the checks that compare them agree on the wrong figure | A drift unseen | Low |
| T21 | The local stack | Information disclosure / integrity: the services' shared data volume, or a key made twice when they start together | Sealed fields that do not open; false audit alerts; data left on a laptop | Low |

SOD: the monthly drill runs as the jobs' account, which can clone only the production instance and change or remove
only instances named for the drill; the quarterly drill is run by the Owner with the break-glass holder and recorded by
the DPO; the registers are kept by the DPO only (Appendix D, Compliance Center C). Founder mode changes none of it. RLS:
the new tenant tables (`restore_drill`, `tick_drill`) force row-level security; the operations list reads only the
caller's rows of `operation`, which already does.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | The exports' buckets live in `fyniq-audit`, uniform access and public access prevention enforced, encrypted with a key in `fyniq-audit`; Cloud SQL's service agent may only create objects; no standing reader: the quarterly drill grants its project's account read and decrypt for the drill only, and removes them (runbook) | `production_exports_outlive_the_production_project`, `staging_is_not_locked` (module tests) `[live-verify]`; `TestTheWeeklyExportAndTheMonthsCopy`, `TestCloudSQLExportsAndWaits` |
| T2 | The exports outlive the production project: locked retention, 35 days weekly and 365 days monthly, in another project; production's database is regional with point-in-time recovery and deletion protection, and a zonal one is refused | `production_is_highly_available_and_recoverable`, `a_zonal_production_database_is_refused` (module tests) `[live-verify]` |
| T3 | Two custom roles with IAM conditions: clone only the production instance by its exact name; get, update and delete only instances whose names start `<instance>-drill-`; the adapter itself refuses to remove any other name | `the_drill_clones_and_removes_only_its_copies` (module test) `[live-verify]`; `TestCloudSQLClonesAtAPointInTimeAndRemovesOnlyItsCopies` |
| T4 | The clone asks for no settings of its own, so it takes the instance's: no public address, the platform's private network only `[live-verify]`; opened by the jobs with the services' own URL and credentials; removed in the same run whatever the result, even when the run is cancelled; the job's lease is 5 hours, and a drill longer than NFR-DR-02's 4 hours is recorded as too slow; the record says whether the copy was removed, and the runbook removes one that was not | `TestTheDrillsCopyIsReachedWithTheServicesOwnURL`, `TestTheRestoreDrill`, `TestCloudSQLClonesAtAPointInTimeAndRemovesOnlyItsCopies`; `docs/runbooks/restore-drill.md` |
| T5 | The registers are insert-only, every change refused (FY323, FY324), each entry audited, kept by the DPO only; the monthly drill's result is the job's own audit event; CTL-RES-02 reads the drill job's last slot (fresh within 32 days) and the quarterly review reads the register | `TestAttackTheRestoreDrillRegisterKeepsItsRules`, `TestAttackTheHeartbeatDrillRegisterKeepsItsRules`, `TestTheRestoreDrillRegister`, `TestTheHeartbeatDrillRegister`, `TestEachReviewReadsItsOwnSource` |
| T6 | On the copy, the drill runs the ledger's verification and the audit chain's, against the anchors kept in `fyniq-audit`: anchors beyond the copy's head are counted as the entries a restore would lose, measured against NFR-DR-01, and a cut or altered chain fails | `TestTheRestoreDrill` |
| T7 | Each committed file is locked under its retention in Locked mode, which nobody can shorten or remove; the bucket keeps versions; the committer's custom role can create, read, list, update and set retention, never delete; staging deletes only `exports/` left uncommitted after 7 days; a daily check reads every committed file's lock and fails CTL-AUD-03 on one missing | `the_records_bucket_locks_each_object`, `nobody_deletes_a_committed_file`, `staging_deletes_only_missed_exports` (module tests) `[live-verify]`; `TestTheRecordsRetentionCheck`, `TestWhatBreaksARecordsRetention`, `TestStorageSendsLockedRetentionAndTemporaryHold`, `TestStorageReadsAnObjectsRetention`, `TestStorageRetentionAgainstTheEmulator` |
| T8 | pgAudit's entries (`AUDIT:` lines of Cloud SQL's `postgres.log`) join the security log sink, 400 days locked in `fyniq-audit` (CTL-AUD-04) | `production_keeps_its_security_logs_in_the_audit_project`, `staging_keeps_them_in_its_own_project` (module tests) `[live-verify]` |
| T9 | Only allow-listed attributes leave: the method, the status, the route's pattern, the scheme, the server, the error type and the `fyniq.*` IDs; span names come from the route's pattern, never the raw path; no query string, header or body; health probes are not traced; log lines carry only the trace and span IDs | `TestSpansGoOutAsOTLPJSON`, `TestALineNamesItsTrace`, `TestLogsNeverCarrySecretsNRICOrBankNumbers` |
| T10 | The trace context is sent only through the platform's own client (Google's APIs and the platform's services); connectors, webhooks and every other host never receive it | `TestOutboundRequestsCarryTheTraceOnlyWithin` |
| T11 | Cloud Armor's rate-based bans and the per-IP and per-client limits bound how many requests a caller can make, and so how many spans it can force; spans are billed per million `[verify]` | `TestEveryFailedRequestIsKeptAndATenthOfTheRest`, `TestATraceContinuesFromItsCaller`; `TestPerIPLimits` |
| T12 | Cloud Monitoring alerts on the heartbeat's absence, outside the app: the tick after 30 minutes to the platform owner and after 60 to both founders; the audit anchor and the heartbeat check after 3 hours. The hourly check compares each job's slots in the database with its schedule and logs a missing one, which alerts. CTL-RES-05's check reads that the scheduler's jobs and the alert policies exist and are on; a drill in staging each quarter proves the alert reaches people | `TestEveryHeartbeatIsLoggedAndEveryMissingOne`, `TestWhatLeavesAStoppedTickUnnoticed`, `TestSchedulingReadsTheJobsAndThePolicies`; `the_heartbeat_tells_the_platform_owner_then_the_owners`, `without_a_named_platform_owner_the_owners_are_told_first` (module tests) `[live-verify]`; `docs/runbooks/heartbeat-drill.md` |
| T13 | The hourly check reads the job slots in the database, which a forged log entry cannot fill, and alerts on a slot missed whatever the logs say | `TestEveryHeartbeatIsLoggedAndEveryMissingOne` |
| T14 | An availability SLO (99.9% a month, 5xx bad) and a latency SLO (95% within 300 ms over 28 days) per public service, on Cloud Run's metrics, which count every request; a fast burn (14.4× over an hour) is critical, a slow one (6× over 6 hours) a warning; no policy without a service | `the_slos_page_the_founders`, `nothing_watched_no_policy` (module tests) `[live-verify]` |
| T15 | The harness exists only in the local build (the release build refuses it); it grows a synthetic organisation through the posting engine and signs its calls with the local dev tokens, which exist only in the local build; CI runs it on the local stack with no cloud credentials and read-only permissions | `TestTheHarnessGrowsALedgerThroughTheEngine`, `TestTheMeasurerCallsTheAPIAsTheWebAppDoes`, `TestWorkflowsPinActionsAndLimitTokens` |
| T16 | An alert on a Cloud Run service billed through an hour with no request (production's scanner excepted, a listed fixed fee); budgets alert at 50%, 80% and 100% and never cap; the cost review before go-live and each quarter | `compute_billed_while_idle_alerts_the_founders`, `alerts_and_never_a_cap` (module tests) `[live-verify]`; `docs/runbooks/cost-review.md` |
| T17 | The list reads only the operations the caller asked for, in the caller's organisation; another person's operation is 404 when polled; the tray reads exports through the caller's own list | `TestMyOperationsAreListed`, `TestLongWorkIsPolledAsAnOperation`; the matrix rows `listMyOperations`, `getOperation` |
| T18 | A list of supported browsers (NFR-BROWSER-01) that the build targets, and the smoke test in Chromium, Firefox and WebKit | `src/browsers.test.ts`; `make test-e2e` with `FYNIQ_E2E_BROWSERS=all` |
| T19 | The words of a search are escaped (LIKE's escape character, `%` and `_`) and only narrow the documents on the trigram indexes; the whole text is then matched as typed, as before; every table keeps its row-level security, the indexes led by the organisation | `TestTheSearchFindsTheTextHoweverItsWordsFall`, `TestTheGlobalSearchFindsWhatThePersonMaySee`, `TestTenantKeysIndexesAndForeignKeys` |
| T20 | Reports read the cache only for a period with nothing later than their revision; a check and the close read the statements from the lines alone (`storage.WithLines`), the trial balance's check from the cache, and `ledger.balances-match-lines` compares the two; the nightly verification too | `TestTamperedBooksFailTheirChecks`, `TestTheCloseAndTheCatalogueAgree`, `TestAPeriodsBalancesAsAtARevisionAreItsLinesUpToIt` |
| T21 | One named volume, the local stand-in for the cloud's buckets and keys, mounted by the API, the worker and the executor alone, the read-only root and the rest of the hardening kept; a key made once, written whole and linked into place; local only, with synthetic data (DEV-08), and `make reset` deletes it | `TestComposeHardensOurServices`, `TestServicesStartingTogetherHoldOneKey` |

## 4. What remains

- Everything in the cloud is code with module tests until the dev cloud: the export and its lock, the keys, the
  clone and its removal, the evidence buckets, the sink, Cloud Trace, the SLOs and every alert `[live-verify]`
  (`p1-launch-readiness` lists them).
- **Built in task 13** (the owner's decisions of 11 Nov 2026, rev 0.250 to 0.252):
  - the evidence buckets and the sealed fields' key ring in `fyniq-audit`, so losing production's project loses neither
    the documents nor the means to open sealed fields (T2) `[live-verify]`;
  - no traces from production until Cloud Trace is shown to keep them in Singapore (CTL-DP-07, T9) `[live-verify]`;
  - `If-Match` accepts the API's own tag as a compressing proxy changes it, weak or with its encoding named (FR-INT-09);
  - every gated performance budget met by the local 1 M-line run; the 10 M-line run as release evidence (QA-PERF-01).
    Whether GitHub's runners hold a 10 M-line database within the release workflow's time and disk is `[verify]`;
    a larger runner is the remedy if not.
- `pg_trgm` and `btree_gin` on Cloud SQL, created by the migrator, `[live-verify]` (T19).
- A clone left by a crash between cloning and removing is found through the drill job's failed slot (CTL-RES-02) and
  the budget alerts, and removed by runbook; nothing sweeps it automatically (T4).
- A caller can force its own requests to be traced (T11); the rate limits bound the cost, and the trace context from
  the internet is still followed so the render and scanner services' spans join the api's.
- A heartbeat entry can be forged by anyone who can write the project's logs (T13); the hourly check of the job slots
  does not depend on the logs.
- Operations carry no percentage and cannot be cancelled yet: the tray shows indeterminate progress.

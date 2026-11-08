# Tasks

Each slice below is one pull request that passes `make test` on its own. Each one carries:
- its migration and attack tests;
- its API, with authorisation-matrix rows on Appendix D's "Audit trail" row;
- its screens, with help keys and guide sections compiled into the knowledge base;
- the tests behind every figure and every message it adds (`// req:` tags).

Before each slice, load `fyniq-feature-change` and `fyniq-security-compliance`. Load `fyniq-accounting-domain` and
`fyniq-quality-gates` for slices 8 and 9, and `fyniq-design-system` for slice 11. Slices 7 to 10 are labelled
`needs-human-review`: they delete from the trail or from posted history.

## 1. The decisions and proof-only values (slice 1)

- [x] 1.1 PRD-001 rev 0.230 records the owner's decisions of 8 Nov 2026, and the main specs are updated and stamped
  (`python3 scripts/prdspec.py stale` is clean, `make check` passes):
  - AUD-02: proof only;
  - AUD-06: security events' details erased after 400 days;
  - the new AUD-10: the ledger's purge, built with the trail's.

  The build plan places AUD-10 here (`make roadmap`). `// req: AUD-02`, `// req: AUD-06`, `// req: AUD-10` (spec text)
- [x] 1.2 The settings `retention.security_events_days` (`Days`, 400, from 30 to 3,650) and
  `retention.purge_after_retention` (`Switch`, off), both under dual control, each with its guide text. The guide says
  the switch purges the trail and the ledger after `retention.records_years` and can never be undone. A settings test
  reads both defaults, and a change by one person waits for a second. `// req: AUD-06`, `// req: AUD-09`,
  `// req: AUD-10`
- [x] 1.3 Fingerprinted values (D1):
  - `audit.New` marks the sensitive fields (identifier names and pay names), and the unit of work replaces each value
    with `{"fingerprint", "hint"}` through `ports.Cipher`;
  - the event format `fyniq-audit-event@2` is documented in `contracts/audit/` next to v1.

  Tests: a payee's bank number changed, changed back and unchanged gives three different, equal and equal
  fingerprints. No body or log line holds the number or a salary. A v1 event still verifies. `// req: AUD-02`,
  `// req: AUD-01`, `// req: SEC-15`
  - As built (9 Nov 2026, rev 0.231):
    - `audit.Prove` runs in the unit of work before masking, and masking leaves a proof alone.
    - Pay names match whole field names, so a forecast's `salary_increase` stays a rate.
    - The store takes the deployment's `ports.Cipher` (`postgres.WithFingerprints`) within the scope
      `audit:<organisation>`, and the wiring passes it in both modes. A store without it proves under a random key of
      its own, so a value is still never kept.
    - [live-verify] The worker and jobs accounts have no role on the fingerprint key in the cloud yet, so their
      proofs do not compare with the API's until they are granted `macSign` on that key alone.
    - Tests: `TestSensitiveValuesAreProvedNeverKept`, `TestSensitiveValuesReachTheTrailAsProofs`,
      `TestEveryEventMatchesTheAUD01Schema` (each event against the schema it names) and
      `TestTheAuditTrailsRetentionShipsAsDecided`. The settings' dual control is the catalogue's own, as for every
      FM-1 setting.

## 2. Security events' details (slice 2)

- [x] 2.1 `audit.SecurityActions` (D2), and a conventions test that refuses an emitted action matching a security
  pattern but not listed. `fyniq.audit_request_detail` is insert-only, and a trigger refuses deleting a row younger
  than the setting allows. A security event's body carries the details' fingerprint, and verification checks it while
  the row exists. Attack tests:
  - the API role deletes a detail;
  - the worker role deletes a young detail;
  - an altered IP fails verification.

  `// req: AUD-06`, `// req: AUD-00`
- [x] 2.2 The `audit.erase` job (daily, worker role). On a clock 401 days on, it erases the details older than the
  setting and writes `audit.details_erased` and a signed erasure-register entry to the write-once bucket. The event
  stays and verifies. A C.2 check warns when the job's last run is more than two days old. `// req: AUD-06`
  - As built (9 Nov 2026, rev 0.232):
    - **Classes.** `audit.IsSecurity` names security events by prefix and suffix:
      - sign-ins and sign-outs, identity links and passkeys, sign-in methods and recoveries;
      - a Super admin's events;
      - report exports, and every `.exported` and `_revealed` action.

      `TestSecurityLookingActionsAreSecurityEvents` (archtest) finds every action the code writes, and refuses one
      that `audit.LooksSecurity` matches but `IsSecurity` does not.
    - **Sign-ins were not in the trail at all** (only in the application log). Now `signin.AuditSignIn` and
      `signin.AuditSignOut` write `session.started` and `session.ended` (with why: signed out, or replaced by a new
      sign-in) to the person's organisation.
      - Their IP travels in a context value private to the sign-in package, so no other event of the sign-in keeps it
        in its body.
      - `registerSignIn` takes `signin.Hooks`.
    - **The table.** `fyniq.audit_request_detail` (000285) is insert-only. `fyniq.audit_erase_details` (worker only)
      refuses a cutoff less than 30 days ago, and a trigger refuses deleting any younger row, even for the owner (FY303).
    - **Verification.** `audit.VerifyAll` takes the deployment's keyed hash (`Kit.Keyed`). Without one it reports the
      details unchecked.
    - **The job.** The register entry is written first, then the erasure, then `audit.details_erased`. C.2 #12 watches
      `audit.erase` (stale after 50 hours) like every scheduled job, so no new check was needed.
    - **Tests:** `TestSecurityEventsKeepTheirDetailsBesideTheChain`, `TestAttackRequestDetailsKeepTheirRules`,
      `TestSecurityDetailsAreErasedAfterTheirRetention` and `TestSignInsAndSignOutsAreInTheTrail`.

## 3. Every sensitive read and export audited (slice 3)

- [ ] 3.1 The audit kinds `export` and `read` in `internal/api`'s registry, and the hook that writes an event after
  the handler succeeds, failing the response when it cannot. Evidence downloads, the payment file and the brand files
  gain their events. A conventions test walks the generated contract and fails on an operation that carries a file
  (`content_base64`) or a full identifier with no kind, or whose call writes no event. `// req: AUD-05`

## 4. Close snapshots, signed and anchored (slice 4)

- [ ] 4.1 `fyniq-close-snapshot@2` (D4) holds the subledger totals of every control account the close reconciles and
  the chain's head, both read under the ledger lock (ENG-47). A reopen records its before and after snapshots the same
  way. Test: a hand-worked close with receivables, payables, an asset and a director's account, whose snapshot holds
  each total and the head at the close's revision. `// req: AUD-04`, `// req: ENG-47`
- [ ] 4.2 The outbox job `audit.anchor_close` signs and writes the close anchor once, for a close and for a reopen's
  two snapshots. Verification checks the stored snapshot's hash and the head against it. Tests:
  - a changed snapshot fails;
  - a second write is refused by the bucket.

  `// req: AUD-04`, `// req: CTL-AUD-02`

## 5. Verify integrity (slice 5)

- [ ] 5.1 One range verification (D5) serves the nightly job and `GET /v1/audit/verification?from=&to=`, with
  matrix rows (V on "Audit trail"). It answers passed, failed with the first bad sequence and its reason, or
  explained, and writes `audit.verified_by_member`. Tests:
  - a clean range passes;
  - a tampered body, a removed event, a changed anchor and a bad signature each fail at the right sequence;
  - a range over 366 days is refused with its code.

  `// req: AUD-03`, `// req: INV-12`

## 6. The explorer and its export (slice 6)

- [ ] 6.1 `GET /v1/audit/events` (cursor-paged, with every filter of D6) and `GET /v1/audit/events/{id}`, with the
  explorer's expression indexes (created concurrently) and matrix rows. Tests:
  - each filter alone and together;
  - before → after values;
  - a sensitive value as changed, changed back and unchanged, with its hint;
  - a security event's details, then "erased on" after slice 2's job;
  - the AI and the roles without V refused.

  `// req: FR-CTL-03`, `// req: AUD-07`, `// req: AUD-02`
- [ ] 6.2 The `audit_trail` report export (CSV and XLSX, the list's filters), audited as an export. Test: the export's
  rows equal the list's for the same filters, and the export writes its event. `// req: FR-CTL-03`, `// req: AUD-07`,
  `// req: AUD-05`

## 7. The trail's purge (slice 7)

- [ ] 7.1 `fyniq.audit_boundary` and `fyniq.audit_purge` (D7a). Attack tests:
  - a direct delete;
  - a purge without a boundary, past one, with the switch off, or as `fyniq_api`;
  - the flag set by hand;
  - TRUNCATE.

  Each is refused, and AUD-08 holds everywhere else. `// req: AUD-09`, `// req: AUD-08`
- [ ] 7.2 The trail's part of `retention.purge`. On a clock past the retention, with the switch on, it purges the
  leading expired segment behind a signed, anchored boundary, and verification starts from it. With the switch off,
  or with one event not yet expired at the front, nothing goes. `// req: AUD-09`, `// req: AUD-03`

## 8. The ledger's carry-forward (slice 8)

- [ ] 8.1 `internal/domain/retention` (D7b): the year plan (the oldest year past its retention, every period locked,
  no legal hold) and the pure carry-forward rule `purge-carry-forward@1`. Hand-worked tests:
  - balance-sheet accounts by dimension set and currency;
  - an open invoice and an open bill carried per document;
  - a settled invoice carrying nothing;
  - revenue and expense into retained earnings;
  - a foreign-currency base residual as a base-only line;
  - a share issue's entry kept.

  The package joins archtest's money packages. `// req: AUD-10`, `// req: INV-01`, `// req: INV-07`
- [ ] 8.2 The oracle's purge, written from AUD-10, and the differential source `purge`. Hypothesis generates ledgers
  with open items, foreign currency, dimensions and reversals, and the engine and the oracle agree to the cent with
  the unpurged ledger at every date from the boundary on. It has golden cases, and the full `make differential` is
  clean. `// req: AUD-10`, `// req: QA-ENG-02`

## 9. The ledger's purge (slice 9)

- [ ] 9.1 `fyniq.ledger_boundary`, `period.purged_on` and `fyniq.ledger_purge` (D7b). Attack tests:
  - an update or delete of a posting (INV-02);
  - a posting into a locked period (INV-03);
  - a purge without a boundary, with the switch off, as `fyniq_api`, out of year order, or with the flag set by hand.

  Each is refused. `// req: AUD-10`, `// req: INV-02`, `// req: INV-03`
- [ ] 9.2 The ledger's part of `retention.purge` and its readers:
  - the job compares the trial balance and sub-ledgers at the boundary before and after, in its own transaction, and
    refuses to commit on a difference;
  - the balance service refuses earlier dates and revisions with `BALANCE-PURGED`;
  - a document whose postings were purged says so;
  - the nightly ledger verification passes.

  Tests: a company purged in 2099, whose every report from the boundary on is unchanged and earlier ones refused.
  `// req: AUD-10`, `// req: INV-08`, `// req: ENG-46`

## 10. Restore behind the anchors (slice 10)

- [ ] 10.1 Anchor epochs, `fyniq.audit_recovery` and the recovery step (D8), run by the nightly verification and as
  `make audit-recover`. A restore simulated behind published anchors writes a signed recovery event, starts a new
  epoch, raises the alert, the ✖ check and the notifications, and verification reports the gap as explained.
  - G4-071: a purge followed by a restore behind the last anchor verifies the rest from the boundary and explains the
    missing range.
  - The runbook is `docs/runbooks/audit-restore.md`.

  `// req: AUD-09`, `// req: AUD-03`, `// req: G4-071`
- [ ] 10.2 The erasure register re-applied by the recovery step (G4-070's audit part). Test: details erased, a backup
  from before simulated, the register re-applied, and the erased details gone again before the copy opens.
  `// req: AUD-09`, `// req: G4-070`

## 11. The screen (slice 11)

- [ ] 11.1 Compliance › Audit trail (Expert, `/compliance/audit-trail`): filters, event rows, the side peek with
  before → after (sensitive values as changed and their hint, security details or their erasure date), Verify
  integrity with its three outcomes, and export.
  - The registry entry, help keys, and the guide page `audit-trail.md`, compiled into the knowledge base.
  - The inventory row's roles follow Appendix D.
  - A component test with axe covers each state of the inventory: verifying, verified, failed and explained gap.

  `// req: FR-CTL-03`, `// req: AUD-07`, `// req: AUD-03`
- [ ] 11.2 The fragment `AuditTrail.html` checked against the build, and the compliance canvas republished where they
  differ (Recipe C). This waits for the canvas hold to end, on 10 Nov 2026 06:00 SGT, and for access to the canvas from
  the new account. `python3 docs/design/screens/build.py check` passes.

## 12. Threat model and as built (slice 12)

- [ ] 12.1 The threat model `docs/security/threat-models/audit-trail.md` covers:
  - fingerprints and their key;
  - the details beside the chain and their erasure;
  - the audit hook;
  - close anchors and verification;
  - both purges and their functions;
  - restore and recovery.

  Each threat has its control and its test.
- [ ] 12.2 `make test` and `make up` pass on the last slice, `make differential` is clean, and `make check` passes. The
  build plan's p1-audit-explorer paragraph says what was built.

## Workflow follow-up

- `/opsx:verify p1-audit-explorer`, then `/opsx:archive p1-audit-explorer --skip-specs` once every task above is
  ticked.
- `p1-pdpa-retention` extends the erasure register to per-person cohort keys (§11.8), and the data inventory states
  the 400 days' reason (CMP-PD-02).

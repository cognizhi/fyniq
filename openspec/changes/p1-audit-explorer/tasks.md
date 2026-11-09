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

- [x] 3.1 The audit kinds `export` and `read` in `internal/api`'s registry, and the hook that writes an event after
  the handler succeeds, failing the response when it cannot. Evidence downloads, the payment file and the brand files
  gain their events. A conventions test walks the generated contract and fails on an operation that carries a file
  (`content_base64`) or a full identifier with no kind, or whose call writes no event. `// req: AUD-05`
  - As built (9 Nov 2026, rev 0.233):
    - **No hook was needed.** Every such handler already reads in a unit of work, so it records its event there, and the
      answer never goes out unless the event was saved. `auditedReads` names each operation's action, all of them
      security events (AUD-06):
      - the five handler exports' `report.exported` and `access_review.exported`;
      - `evidence.opened` and `payment_batch.file_downloaded`, newly security events;
      - the report export's request and download;
      - the three reveals.
    - **The gap found.** The template bundle export wrote nothing; it now writes `document_template_bundle.exported`.
    - **The exemption.** The brand files are `unauditedFiles`: the organisation's own logo and letterhead.
    - **Tests.** `TestEveryFileAndRevealIsAudited` walks the contract's success schemas for `content_base64`, and
      takes every `reveal*` and `export*` operation by name. The authorisation matrix checks that each allowed call of
      a registered operation wrote its action.

## 4. Close snapshots, signed and anchored (slice 4)

- [x] 4.1 `fyniq-close-snapshot@2` (D4) holds the subledger totals of every control account the close reconciles and
  the chain's head, both read under the ledger lock (ENG-47). A reopen records its before and after snapshots the same
  way. Test: a hand-worked close with receivables, payables, an asset and a director's account, whose snapshot holds
  each total and the head at the close's revision. `// req: AUD-04`, `// req: ENG-47`
- [x] 4.2 The outbox job `audit.anchor_close` signs and writes the close anchor once, for a close and for a reopen's
  two snapshots. Verification checks the stored snapshot's hash and the head against it. Tests:
  - a changed snapshot fails;
  - a second write is refused by the bucket.

  `// req: AUD-04`, `// req: CTL-AUD-02`
  - As built (9 Nov 2026, rev 0.234):
    - **No outbox job.** The hourly `audit.anchor` job anchors each close and reopening not yet anchored, before the
      chain head, so a close is anchored within the hour. This needs no outbox plumbing into the close or into the
      approval executor's reopening, and a missing write-once object is what "not yet anchored" means.
    - **Prefixes.** Close anchors live under `audit-close-anchors/`, apart from the chain anchors.
    - **The head.** The API reads it through `fyniq.audit_chain_head` (000286), since it may not read the trail.
    - **Control accounts.** The subledger totals cover every account flagged as a control account, and the
      receivables' and payables' by role, as the close's checks find them.
    - **Test:** `TestClosesAreSignedAndAnchored`, with an invoice, a bill and a journal. It checks:
      - the snapshot's totals (1,000.00 and −327.00) and the head's hash;
      - one anchor per close, the reopening's anchor, and verification;
      - a forged anchor refused.

## 5. Verify integrity (slice 5)

- [x] 5.1 One range verification (D5) serves the nightly job and `GET /v1/audit/verification?from=&to=`, with
  matrix rows (V on "Audit trail"). It answers passed, failed with the first bad sequence and its reason, or
  explained, and writes `audit.verified_by_member`. Tests:
  - a clean range passes;
  - a tampered body, a removed event, a changed anchor and a bad signature each fail at the right sequence;
  - a range over 366 days is refused with its code.

  `// req: AUD-03`, `// req: INV-12`
  - As built (9 Nov 2026, rev 0.235):
    - **Shared links, separate checks.** `audit.VerifyRange` checks the events that occurred on the range's days in the
      organisation's time zone. `Chain` became `ChainAfter`, so a range is linked from the event before it, and the
      nightly job uses the same link check over the whole chain.
    - **What a range checks.** Its anchors, close anchors and security details. The answer gives `anchored_through`:
      later events are anchored within the hour.
    - **Grants.** The API reads its organisation's trail now (000287, under row-level security). Its dependencies take
      the anchors' bucket, the signer and the keyed hash [live-verify: the api account's read of the bucket and the
      key's public key].
    - **Test:** `TestVerifyIntegrityForARange` covers:
      - a clean range and an empty one;
      - a journal's event changed in place, failing at its sequence, and passing once undone;
      - backwards, too-long and malformed ranges, and a Bookkeeper, all refused;
      - four verifications in the trail.

      The matrix row `getAuditVerification` covers the "Audit trail" row.

## 6. The explorer and its export (slice 6)

- [x] 6.1 `GET /v1/audit/events` (cursor-paged, with every filter of D6) and `GET /v1/audit/events/{id}`, with the
  explorer's expression indexes (created concurrently) and matrix rows. Tests:
  - each filter alone and together;
  - before → after values;
  - a sensitive value as changed, changed back and unchanged, with its hint;
  - a security event's details, then "erased on" after slice 2's job;
  - the AI and the roles without V refused.

  `// req: FR-CTL-03`, `// req: AUD-07`, `// req: AUD-02`
- [x] 6.2 The `audit_trail` report export (CSV and XLSX, the list's filters), audited as an export. Test: the export's
  rows equal the list's for the same filters, and the export writes its event. `// req: FR-CTL-03`, `// req: AUD-07`,
  `// req: AUD-05`
  - As built (9 Nov 2026, rev 0.236):
    - **The list.** `listAuditEvents` filters by `actor`, `ai` (a proposal or `any`), `record_type`, `record_id`,
      `field`, `action` (or a prefix ending in `.`), `approval_id` and `from`/`to` days in the organisation's time zone.
      It pages newest first by sequence, and 000288 indexes the actor, the record and the action concurrently.
    - **One event.** `getAuditEvent` gives each field's before and after, a proof's `changed`, `changed_back` or
      `unchanged` and its hint, and the request with its details `in_body`, `kept` or `erased`. It also gives the
      chain's `prev_hash` and `hash`.
    - **The export.** `requestAuditTrailExport` (V on "Audit trail") asks for the `audit_trail` report export (000289
      and 000290 widen its kind check), which the worker prepares oldest first, at most 100,000 events. The generic
      `requestReportExport` still refuses that kind, and the download is the report exports' own, by the caller only.
    - **Tests.** `TestTheAuditTrailExplorer` covers each filter, paging, the three proofs, kept details, the export's
      rows and its audit event, and a Bookkeeper refused. The matrix rows cover `listAuditEvents`, `getAuditEvent` and
      `requestAuditTrailExport`, whose call the matrix checks was audited.

## 7. The trail's purge (slice 7)

- [x] 7.1 `fyniq.audit_boundary` and `fyniq.audit_purge` (D7a). Attack tests:
  - a direct delete;
  - a purge without a boundary, past one, with the switch off, or as `fyniq_api`;
  - the flag set by hand;
  - TRUNCATE.

  Each is refused, and AUD-08 holds everywhere else. `// req: AUD-09`, `// req: AUD-08`
- [x] 7.2 The trail's part of `retention.purge`. On a clock past the retention, with the switch on, it purges the
  leading expired segment behind a signed, anchored boundary, and verification starts from it. With the switch off,
  or with one event not yet expired at the front, nothing goes. `// req: AUD-09`, `// req: AUD-03`
  - As built (9 Nov 2026, rev 0.237):
    - **The database's checks (000291).** The delete guard on `audit_event` checks the purge's flag, a boundary
      covering the event, the switch (`fyniq.purge_switched_on`) and an age of at least 7 years. So even the table's
      owner, with the flag set by hand, deletes nothing the purge could not. `audit_purge` checks the same before it
      deletes the events and their details.
    - **The boundary.** It names its last event by sequence and hash, moves forward only, never changes (FY304), and
      records the setting change that switched the purge on.
    - **The job.** `retention.purge` runs weekly; C.2 #12 watches it, stale after 8 days.
    - **Verification.** It starts from the latest boundary, after checking its signed copy. A range over purged days
      is explained.
    - **Legal holds.** None exist on records yet (only on stored files), so none stops a purge. When records can be
      held, the purge stops at the first event about one.
    - **Tests:** `TestTheTrailIsPurgedBehindASignedBoundary` and `TestAttackThePurgeKeepsItsRules`.

## 8. The ledger's carry-forward (slice 8)

- [x] 8.1 `internal/domain/retention` (D7b): the year plan (the oldest year past its retention, every period locked,
  no legal hold) and the pure carry-forward rule `purge-carry-forward@1`. Hand-worked tests:
  - balance-sheet accounts by dimension set and currency;
  - an open invoice and an open bill carried per document;
  - an invoice reversed under its own document carrying nothing, and a settled one carrying its line and its
    receipt's;
  - revenue and expense into retained earnings;
  - a foreign-currency base residual as a base-only line;
  - a share issue's entry kept.

  The package joins archtest's money packages. `// req: AUD-10`, `// req: INV-01`, `// req: INV-07`
- [x] 8.2 The oracle's purge, written from AUD-10, and the differential source `purge`. Hypothesis generates ledgers
  with open items, foreign currency, dimensions and reversals, and the engine and the oracle agree to the cent with
  the unpurged ledger at every date from the boundary on. It has golden cases, and the full `make differential` is
  clean. `// req: AUD-10`, `// req: QA-ENG-02`
  - As built (9 Nov 2026, rev 0.238):
    - **The rule made precise first.** AUD-10 (rev 0.238) and design D7b now say which entries are kept (also those a
      reconciliation of the bank names, and both ends of every reversal touching a kept entry), how revenue and expense
      carry (into retained earnings by dimension set, in the base currency), how each document's entry balances
      (through suspense, which nets to zero), the rate of a carried foreign balance (base ÷ amount to 10 decimals, 1
      when that is not above zero or has more than 10 integer digits) and the order of the entries and their lines.
    - **The year plan** (`NextYear`) takes the books' first kept line, the periods, the held records' dates and the
      retention (at least 7 years). Records cannot be held yet, so slice 9 passes none. `Kept` is the kept set.
    - **The rule** (`CarryForward`) refuses unbalanced purged lines, a line on no account, a base in another currency,
      and a retained earnings or suspense account that is a control account, revenue or expense.
      `retention` joins archtest's money packages.
    - **The oracle** (`oracle/src/oracle/purge.py`) is written from D7b and the README's "Purges". It computes the
      positions at every date from the boundary on (the boundary, each later posting date and each year's first
      day), with the year-end fold and the open items by document, for the purged and the unpurged ledger.
    - **The format** is `fyniq-purge@1` and `fyniq-purge-result@1` (`purge.schema.json`, README "Purges"). Both
      sides refuse the same inputs (`TestParsePurgeRefusesWhatTheOracleRefuses`, `test_what_the_oracle_refuses`).
    - **Persons and instruments.** Building it showed that a line on a director's account or share application monies
      names its person, and one on convertible instruments its SAFE; the database requires them there and refuses them
      elsewhere. So every key but revenue and expense's also holds the line's person and instrument (AUD-10 and D7b
      say so), or the sub-ledgers by person (INV-13) and by instrument (FR-EQ-18) would merge.
    - **The goldens** in `test/differential/purges/`:
      - `hand-worked-year`: the year of `TestTheCarryForwardOfAYearByHand`, a named share issue, and a reversal
        across the boundary;
      - `foreign-residuals`: each rate fallback;
      - `persons-and-instruments`: two directors' loans in one document, and two SAFEs;
      - `shrunk-cdb82bff61`: the first full run's find, a chain of reversals reaching a year after the boundary,
        whose first day the engine's side skipped (a harness bug, fixed).
    - **The gate.** `TestThePurgeAgreesWithTheOracle` runs the goldens and 40 generated ledgers in `make test`, and
      requires every position unchanged. Hypothesis generates ledgers with control accounts, foreign and base-only
      lines, dimension sets, shared documents, reversal chains and named entries. `make differential` draws them
      (`purge` in `DIFFERENTIAL_SOURCES`).

## 9. The ledger's purge (slice 9)

- [x] 9.1 `fyniq.ledger_boundary`, `period.purged_on` and `fyniq.ledger_purge` (D7b). Attack tests:
  - an update or delete of a posting (INV-02);
  - a posting into a locked period (INV-03);
  - a purge without a boundary, with the switch off, as `fyniq_api`, out of year order, or with the flag set by hand.

  Each is refused. `// req: AUD-10`, `// req: INV-02`, `// req: INV-03`
- [x] 9.2 The ledger's part of `retention.purge` and its readers:
  - the job compares the trial balance and sub-ledgers at the boundary before and after, in its own transaction, and
    refuses to commit on a difference;
  - the balance service refuses earlier dates and revisions with `BALANCE-PURGED`;
  - a document whose postings were purged says so;
  - the nightly ledger verification passes.

  Tests: a company purged in 2099, whose every report from the boundary on is unchanged and earlier ones refused.
  `// req: AUD-10`, `// req: INV-08`, `// req: ENG-46`
  - As built (9 Nov 2026, rev 0.238):
    - **The rule, refined while building** (AUD-10 and D7b say so):
      - every line carries under its own document, not only a control account's: the reconciliations of an account with
        its sub-ledger read it by source type (deposits, the spreads' holding accounts, the asset register as well as
        receivables and payables), and a carried total under `ledger_boundary` would count twice. A document's revenue
        and expense carry into retained earnings in its own entry, which so balances as its entries did: no suspense
        line, and no boundary entry (a suspense account netting to zero would still show in the trial balance);
      - the carry-forward is dated the day before the boundary, in the purged year's last period, so every report from
        the boundary on reads it as its opening balance and none as a movement;
      - the asset register's depreciation and disposals and the equity register's entries are kept, as share issues
        are: the register reads an asset's months for as long as it is held;
      - the boundary is a year's first day of the financial-year calendar (its first year may be longer or shorter),
        and the purge waits until the organisation's financial years are set, as year-end folds nothing without them.
    - **The database (000292).** `fyniq.ledger_boundary` (guarded, stamped with its transaction, committed only with its
      purge), `fyniq.ledger_purged_entry` (a tombstone per purged entry, never changed), `period.purged_on`,
      `fyniq.ledger_purge(boundary, entries)` and FY305. `journal_entry_assign`, `journal_line_check`,
      `journal_line_base_only`, the release and depreciation triggers, `journal_immutable` and `period_guard` admit the
      purge's statements and nothing else. Tests: `TestTheLedgerIsPurgedAndEveryReportFromTheBoundaryKept` (two
      successive years, every position and general ledger unchanged, opening balances included) and
      `TestAttackTheLedgerPurgeKeepsItsRules`.
    - **The check of the carry-forward is the database's**: `ledger_purge` refuses to delete unless the carry-forward
      its transaction posted replaces the purged lines exactly, by account, set, currency, person, instrument and
      source (revenue and expense folded into retained earnings). No adapter query sums journal lines (ENG-04).
    - **The job** (`retention.PurgeLedger`, run by `retention.purge` before the trail's): one transaction per
      organisation; it posts through the posting service (`journal.CarryForward`) and compares the balances cache with
      the lines.
    - **The readers.** `BALANCE-PURGED` (409) from every dated reading of the balance service before the boundary, a
      purged period's trial balance and an "as at" revision before the purge; `Ledger.ByIdentity` falls back to the
      document's carry-forward; `Ledger.PurgedOfSource`. `TestTheReadersAfterTheLedgerPurge`: receivables with
      invoices of 2010 (one in USD, one voided) reconcile as at 30 June 2011 exactly as before, and the nightly
      verification passes. The screen's list of boundaries and each document's note wait for slice 11 (task 11.1).
    - **Not checked here:** the anchors' bucket and its signing key in the cloud (`[live-verify]`, as slice 7's).

## 10. Restore behind the anchors (slice 10)

- [x] 10.1 Anchor epochs, `fyniq.audit_recovery` and the recovery step (D8), run by the nightly verification and as
  `make audit-recover`. A restore simulated behind published anchors writes a signed recovery event, starts a new
  epoch, raises the alert, the ✖ check and the notifications, and verification reports the gap as explained.
  - G4-071: a purge followed by a restore behind the last anchor verifies the rest from the boundary and explains the
    missing range.
  - The runbook is `docs/runbooks/audit-restore.md`.

  `// req: AUD-09`, `// req: AUD-03`, `// req: G4-071`
- [x] 10.2 The erasure register re-applied by the recovery step (G4-070's audit part). Test: details erased, a backup
  from before simulated, the register re-applied, and the erased details gone again before the copy opens.
  `// req: AUD-09`, `// req: G4-070`
  - As built (9 Nov 2026, rev 0.239):
    - **The database (000293).** `fyniq.audit_recovery`: the epoch it starts (the next one only), the restored head
      and its hash, the range no longer proven, when it was found and its signed copy; it never changes or goes
      (FY306). Its acknowledgement is a row of its own, once (`fyniq.audit_recovery_acknowledgement`), so both stay
      append-only as the trail is (AUD-08); each is audited (`audit.restored`, `audit.restore_acknowledged`, named so
      as not to read as an account's recovery, a security event).
    - **The recovery step** (`audit.Recover`, `RecoverAll`; `jobs audit-recover`, `make audit-recover`; first in
      `audit.Nightly`, the nightly `audit-verify`). It compares the chain with the current epoch's anchors from the
      latest purge boundary on: the restored head is the last anchored event that matches, the range runs from the
      next to the last anchored. A chain that does not link, or an anchor whose signature does not verify, is left to
      verification as tampering. The signed recovery goes first to `audit-anchors/<org>/recovery/<epoch>.json`.
      The nightly job fails after recovering, so the ops alert is raised.
    - **Epochs.** `AnchorAll` writes under the current epoch (`e<n>/`); verification and Verify integrity check every
      anchor of the current epoch and each earlier epoch's up to its recovery's restored head, and count the rest as
      explained; a range over a lost range is explained by the recovery.
    - **The notice and the check.** `security.audit_recovered` (security, critical, emailed at once) to the Owners
      and DPOs. The check of the audit trail (C.2 #3) fails, naming each recovery waiting, until the Owner
      acknowledges it (`acknowledgeAuditRecovery`, A on "Audit trail" by the Owner, signed in, with If-Match;
      `listAuditRecoveries`, V). The check now reads the trail's latest verification when none came since its last
      run, so an acknowledgement clears it at once.
    - **Tests:** `TestARestoreBehindTheAnchorsIsRecovered`, `TestAPurgeThenARestoreBehindTheLastAnchor` (G4-071),
      `TestTheErasureRegisterIsReapplied` (G4-070), `TestAttackARecoveryKeepsItsRules`,
      `TestARecoveryFailsTheAuditCheckUntilAcknowledged`, `TestARecoveryIsListedAndAcknowledgedByTheOwner` and the
      matrix rows. The restore is simulated by an anchor published for events the database no longer holds.
    - **Not checked here:** a real point-in-time restore in the cloud and the bucket's retention lock
      (`[live-verify]`); the screen's view of recoveries waits for slice 11.

## 11. The screen (slice 11)

- [x] 11.1 Compliance › Audit trail (Expert, `/compliance/audit-trail`): filters, event rows, the side peek with
  before → after (sensitive values as changed and their hint, security details or their erasure date), Verify
  integrity with its three outcomes, and export.
  - The registry entry, help keys, and the guide page `audit-trail.md`, compiled into the knowledge base.
  - The inventory row's roles follow Appendix D.
  - A component test with axe covers each state of the inventory: verifying, verified, failed and explained gap.
  - The ledger's boundaries listed for it (`GET /v1/ledger/boundaries`), and each document's view saying when its
    postings were purged after retention (`Ledger.PurgedOfSource`, slice 9).
  - The recoveries from restores behind the anchors, each waiting one with the Owner's acknowledgement
    (`listAuditRecoveries`, `acknowledgeAuditRecovery`, slice 10).

  `// req: FR-CTL-03`, `// req: AUD-07`, `// req: AUD-03`

  **What building added:**
    - **One list for both purges:** in place of `GET /v1/ledger/boundaries`, `listAuditPurges` (`GET /v1/audit/purges`,
      Audit trail V) lists the trail's and the ledger's purges together, oldest first. With `record_id`, it also lists
      that record's postings from their tombstones.
    - **Recoveries:** `AuditRecovery` gains `version` (1 waiting, 2 acknowledged), so the screen sends `If-Match` without
      reading each one.
    - **Verification:** `problem` also says what left an explained gap.
    - **The screen:** `apps/web/src/screens/compliance/AuditTrail.tsx`, with these parts:
      - a banner for each waiting recovery, with the Owner's Acknowledge;
      - Verify integrity with its outcomes;
      - the explorer as a list and a detail, with the proofs, the hint and the erased details;
      - the export, which lists the person's exports and never asks again for an expired audit export from there;
      - Purges and restores.
    - **Where it is reached:** the Accounting page links to it, beside Evidence, and the check's items open it
      (`audit_chain`, `audit_recovery`).
    - **Records' pages:** a posted journal's, bill's or invoice's page says when the ledger's purge removed its
      postings (`PurgedPostings`, for the roles that view the audit trail). The other records' pages, such as bank
      transactions and expense claims, can add the same component.
    - **The inventory row:** it adds the Administrator, AUD-03 and AUD-10, the purges and restores list, and the state
      "restore waiting for acknowledgement".
    - **The organisation settings guide:** it says where each purge is listed.
    - **Tests:** `AuditTrail.test.tsx` and two `JournalPage` tests, with axe; `TestThePurgesAfterRetentionAreListed`
      and the matrix row.
    - **Not checked here:** the canvas (11.2).
- [ ] 11.2 The fragment `AuditTrail.html` checked against the build, and the compliance canvas republished where they
  differ (Recipe C). This waits for the canvas hold to end, on 10 Nov 2026 06:00 SGT, and for access to the canvas from
  the new account. `python3 docs/design/screens/build.py check` passes.

## 12. Threat model and as built (slice 12)

- [x] 12.1 The threat model `docs/security/threat-models/audit-trail.md` covers:
  - fingerprints and their key;
  - the details beside the chain and their erasure;
  - the audit hook;
  - close anchors and verification;
  - both purges and their functions;
  - restore and recovery.

  Each threat has its control and its test.
  - As built (10 Nov 2026): 15 threats (T1 to T15) across the person, API, worker, bucket and operator boundaries,
    each with its control and the tests that prove it. What remains: the `[live-verify]` keys, grants and retention
    lock; a real point-in-time restore; holds on records; nobody holding A on "Audit trail"; the cohort keys of
    `p1-pdpa-retention`; and the other records' pages without the purge note.
- [x] 12.2 `make test` and `make up` pass on the last slice, `make differential` is clean, and `make check` passes. The
  build plan's p1-audit-explorer paragraph says what was built.
  - As built (10 Nov 2026):
    - **`make differential`** is clean: 4 passed, `purge` among its sources.
    - **`make check`** passes.
    - **`make test`** first failed in `TestAuthorisationMatrix`, locally and in CI on #515: the test database ran out of
      connections (SQLSTATE 53300). Each worker store the matrix opened stayed open until it ended. `workerStore`
      now keeps one per test, fixed on #515's branch, and the whole API suite passes.
    - **`make up`** is checked by CI's local smoke job (QA-LOCAL-01) on this pull request. Locally, another worktree's
      stack holds the compose project `fyniq`, and starting this one would have replaced it.

## Workflow follow-up

- `/opsx:verify p1-audit-explorer`, then `/opsx:archive p1-audit-explorer --skip-specs` once every task above is
  ticked.
- `p1-pdpa-retention` extends the erasure register to per-person cohort keys (§11.8), and the data inventory states
  the 400 days' reason (CMP-PD-02).

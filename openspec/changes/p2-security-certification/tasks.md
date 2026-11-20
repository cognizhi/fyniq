# Tasks

Before each slice load `fyniq-security-compliance` (and `fyniq-feature-change`, `fyniq-design-system`, `fyniq-knowledge-base`). Every grant is listed in
`docs/security/p2-grants-for-human-review.md`; every AUD-11 commit is `needs-human-review`.

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.617 records D1 to D5 `[owner to confirm]`: AUD-11's coverage, SEC-25's register, the new CMP-08, controls CTL-CHG-12 and CTL-AUD-07, two Appendix D rows; specs stamped; `make roadmap`. `// req: AUD-11`, `// req: SEC-25`, `// req: CMP-08`
  Built: the rows, `compliance/controls/CTL-AUD-07.yaml` and `CTL-CHG-12.yaml` (checks planned until 2.1 and 4.2), the specs and `scripts/roadmap.py` (CMP-08).

## 2. Legal holds, the database and the purges (AUD-11)
- [x] 2.1 `retention_hold`, its guards, `hold_covers_event`, `hold_ledger_dates`, `hold_evidence`, the delete triggers on `audit_event` and `journal_entry`, the `ledger_boundary` guard (D1, D2). Attack tests as the purge's own role (`fyniq_worker`) and the API's. `// req: AUD-11`, `// req: SEC-03`
  Built: migration 001300 (the table, its guard, five coverage functions, four delete or insert guards); `TestAHoldKeepsTheTrailFromThePurgeAndTheDatabaseRefusesIt`, `TestAHoldOnARecordOrAContactCoversItsEventsAndEntries`.
- [x] 2.2 `PurgeTrail` stops before the first covered event and `PurgeLedger` passes the held dates to `NextYear`; a purge of held rows is refused by the database. `// req: AUD-11`
  Built: `audit.PurgeTrail`, `retention.PurgeLedger` (`NextYear` takes the held dates); `TestAHoldKeepsTheLedgerYearFromThePurge`.
- [x] 2.3 The storage reconcile `retention.holds`: files covered, files attached later, a lift that keeps a file another hold covers. `// req: AUD-11`
  Built: `retention.SyncHolds` and the nightly job `retention.holds`; `TestTheStoredFilesOfAHeldRecordAreHeldAndFreedWithIt`. The cloud's reading of the lock is `[live-verify]`.
- [x] 2.4 Anonymising and shredding leave a held contact and a held year alone. `// req: AUD-11`, `// req: CMP-PD-05`
  Built: `person_key_hold_guard`, the shredding listing, the anonymise handler; `TestAHeldPersonsKeysAreNotShredded`.

## 3. Legal holds, API and screen
- [x] 3.1 `placeRetentionHold`, `liftRetentionHold`, `listRetentionHolds`; Appendix D row; matrix rows; audit; notice to the Owners and DPO. `// req: AUD-11`
  Built: `internal/api/retention_holds.go`, notice type `retention.hold`, step-up on the lift; `TestLegalHoldsAreRecordedListedAndLifted`, `matrixRetentionHolds`.
- [x] 3.2 The Holds tab on Data protection, help keys, guide section, knowledge page, design fragment; the DPO's Home block; CTL-AUD-07; the threat model's "What remains" and the spec's "After go-live" scenario corrected. `// req: AUD-11`, `// req: QA-UI-08`
  Built: `Holds.tsx`, help keys `data-protection.holds`, `.hold-place`, `.hold-lift`, the guide's "Legal holds" and its knowledge page, the design fragment's tab, the DPO's Home row `holds-in-force`, CTL-AUD-07 running, the threat model and the spec corrected.

## 4. Security assessments (SEC-25)
- [ ] 4.1 `security_assessment` and `security_finding`, guards, API, matrix rows, audit. `// req: SEC-25`
- [ ] 4.2 Verification `security-assessments` and control CTL-CHG-12 (its planned gate for `p3-saas-platform`). `// req: SEC-25`
- [ ] 4.3 The Security assessments screen, help keys, guide page, knowledge page, design fragment. `// req: SEC-25`
- [ ] 4.4 `[live-verify]` The independent test by an outside firm; DAST against staging (CTL-CHG-07's nightly check).

## 5. Readiness by framework (CMP-08)
- [ ] 5.1 `GET /v1/compliance/readiness` from the registry and the dashboard, with Annex A's clause numbers; fixtures for CE, CT, ISO and SOC 2. `// req: CMP-08`
- [ ] 5.2 The Readiness panel on Controls, help, guide, knowledge page, design fragment. `// req: CMP-08`
- [ ] 5.3 `[live-verify]` A certification body's reading of the gaps; the statement of applicability's wording.

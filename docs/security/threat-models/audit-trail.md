# Threat model: the audit trail (proofs, security details, verification, purges, restores)

| | |
|---|---|
| Feature and requirement IDs | The audit trail and the ledger's retention: FR-CTL-03, AUD-01 to AUD-10, INV-02, INV-03, INV-12, ENG-46, ENG-47, G4-070, G4-071, SEC-15, CTL-AUD-02 (PRD-001 rev 0.230 to rev 0.239; `p1-audit-explorer` task 12.1) |
| Author, reviewer, date | Claude Code for the founders, 10 Nov 2026; second review at sign-off (waived before production, AGENTS.md); slices 7 to 10 are labelled `needs-human-review` |
| Data and its class | The audit trail: every saved change with its actor, reason and before and after values (confidential). It holds proofs of sensitive values (bank numbers, identifiers, pay), never the values. Security events' IP addresses and browsers (personal data, 400 days). Close snapshots (confidential figures). The ledger's posted history and its carry-forward (confidential). The anchors, recoveries and erasure register in the write-once bucket. The fingerprint key and the signing key are secrets held by the key service |
| Who acts | Appendix D's "Audit trail" row: V (Owner, Administrator, Controller, Accountant, Auditor, Tax agent, DPO) reads, verifies and exports; the Owner acknowledges a recovery (nobody holds A yet). The worker (`fyniq_worker`) anchors, erases, purges and recovers; the API (`fyniq_api`) writes events in each unit of work and reads its organisation's trail. The AI has no tool on the trail |

## 1. What we are building

- **Proofs, not values (AUD-02).** A sensitive field reaches the trail as `{fingerprint, hint}`: a keyed hash under
  the organisation's scope and the last four characters. The explorer says whether it changed, changed back or not.
- **Security events' details (AUD-06).** A security event's IP address and browser sit beside the chain in
  `audit_request_detail`, with their fingerprint in the event's body. The daily `audit.erase` job erases them after
  the setting's days (400) and writes a signed entry to the erasure register.
- **Every sensitive read and export audited (AUD-05).** Each file download, reveal and export writes its security
  event in the unit of work that reads it, so the answer never goes out unless the event was saved.
- **Close snapshots (AUD-04).** A close or reopening records the control accounts' subledger totals and the chain's
  head, and the hourly `audit.anchor` job signs and writes each one once to the write-once bucket.
- **Verify integrity (AUD-03).** Any range of days up to 366 days is recomputed from the event before it and checked
  against its anchors, close anchors and security details: passed, failed at a sequence, or explained. The nightly job
  checks the whole chain.
- **The explorer and its export (FR-CTL-03, AUD-07).** Filters, one event's before and after, and an export prepared
  by the worker.
- **The purges after retention (AUD-09, AUD-10).** When the organisation switches on
  `retention.purge_after_retention` (dual control), the weekly `retention.purge` job purges the oldest financial year
  past `retention.records_years` (at least 7 years): the ledger's entries, carried forward to the next year's first day
  so every later report is unchanged, and the trail's events of that time. Each purge sits behind a signed boundary
  and leaves tombstones.
- **Restores behind the anchors (AUD-09, G4-070, G4-071).** The recovery step finds a restore that ended before a
  published anchor, records the restored head and the range no longer proven, signs it, starts a new anchor epoch,
  re-applies the erasure register, and alerts. The check of the audit trail fails until the Owner acknowledges it.

Trust boundaries:

- **person ↔ API**: the Appendix D row and SEC-02;
- **API and worker ↔ database**: RLS per organisation, the audit tables' grants and triggers (AUD-08), the purge
  functions' guards (FY303 to FY306);
- **worker ↔ write-once bucket and key service**: anchors, boundaries, recoveries and the erasure register, signed;
- **operator ↔ database**: a restore from a backup.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | API ↔ database | Information disclosure: a bank number, identifier or salary reaches an event's body, a log line or the export in clear | A sensitive value kept for the life of the company in a store everyone with V reads | Medium |
| T2 | API ↔ key service | Information disclosure: proofs compared across organisations, or a fingerprint brute-forced from its hint | A value guessed, or two organisations linked by a shared payee | Low |
| T3 | API ↔ database | Repudiation: a security-looking action (a sign-in, a reveal, an export, a file opened) writes no event, or its answer goes out before its event is saved | No evidence of who read a bank number or exported the books | Medium |
| T4 | worker ↔ database | Tampering and repudiation: security details deleted early, or kept past their retention (PDPA), or erased without a record | Evidence lost, or personal data kept too long | Medium |
| T5 | any role ↔ database | Tampering: an event, a detail, a boundary, a recovery or an acknowledgement changed or deleted in place, or the trail truncated | The trail no longer shows what happened | Low |
| T6 | worker ↔ bucket | Tampering: an anchor, close anchor, boundary or recovery overwritten or forged | Verification passes over an altered trail | Low |
| T7 | person ↔ API | Tampering hidden: verification passes over an altered event, a removed event, a changed anchor or a bad signature, or a gap is called explained without a signed reason | Tampering goes unseen | Low |
| T8 | worker ↔ database | Tampering (excessive deletion): the purge deletes events or postings that are not yet past retention, with the switch off, without a boundary, out of year order, or by anyone but the job | Records the law requires are lost | Low |
| T9 | worker ↔ database | Tampering (wrong figures): the ledger's carry-forward does not replace the purged lines exactly, so a later report, sub-ledger or the balances cache changes | Every report after the purge is wrong | Medium |
| T10 | person ↔ API | Information disclosure and tampering: a report of a purged day, or an "as at" revision before the purge, silently reads the carry-forward as history | A report that looks complete but is not | Medium |
| T11 | operator ↔ database | Repudiation and tampering: a restore behind the anchors goes unnoticed; or a tampering is passed off as a restore | Lost events never explained; or an attacker's rewrite accepted as a recovery | Low |
| T12 | operator ↔ database | Information disclosure: a restore brings back security details erased after the backup was taken | Personal data kept past its retention (G4-070) | Medium |
| T13 | person ↔ API | Elevation of privilege: someone other than the Owner acknowledges a recovery, the AI does, or it is acknowledged twice | The check clears without the runbook | Low |
| T14 | person ↔ API | Information disclosure: the explorer, an event or the export reaches a role without V, the AI or an MCP client | The company's whole change history leaks | Low |
| T15 | person ↔ API | Denial of service: an export of every event, a verification of years, or explorer filters with no index | The worker or the database is tied up | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | `audit.New` marks the sensitive fields; `audit.Prove` replaces each with its proof in the unit of work before masking, under the deployment's cipher (a random key when none, so a value is never kept). The event format is `fyniq-audit-event@2`; the export writes the body as recorded. Logs never carry the value (SEC-15) | `TestSensitiveValuesAreProvedNeverKept`, `TestSensitiveValuesReachTheTrailAsProofs`, `TestEveryEventMatchesTheAUD01Schema`, `TestTheAuditTrailExplorer` (AUD-02, SEC-15) |
| T2 | The keyed hash runs in the key service within the scope `audit:<organisation>`; the key never leaves it; the hint is the last four characters only | `TestSensitiveValuesAreProvedNeverKept` (equal values give equal proofs only within one scope) |
| T3 | `audit.IsSecurity` names security events by prefix and suffix; an archtest refuses an action `LooksSecurity` matches but `IsSecurity` does not. Every file, reveal and export records its event in the unit of work that reads it; the matrix checks each allowed call of a registered operation wrote its action | `TestSecurityLookingActionsAreSecurityEvents`, `TestEveryFileAndRevealIsAudited`, `TestSignInsAndSignOutsAreInTheTrail`, `TestAuthorisationMatrix` (AUD-05) |
| T4 | `audit_request_detail` is insert-only; `audit_erase_details` is the worker's only, refuses a cutoff under 30 days, and a trigger refuses deleting a younger row even for the owner (FY303). The register entry is written and signed first, then the erasure, then `audit.details_erased`; the event stays and verifies | `TestAttackRequestDetailsKeepTheirRules`, `TestSecurityDetailsAreErasedAfterTheirRetention` (AUD-06) |
| T5 | The audit tables are insert-only by grant and trigger (`audit_immutable`, AUD-08), TRUNCATE included; a recovery never changes (FY306) and its acknowledgement is a row of its own, once | `TestAttackChangesWithoutAnAuditEventFail`, `TestTamperingWithTheTrailIsDetected`, `TestAttackThePurgeKeepsItsRules`, `TestAttackARecoveryKeepsItsRules` (AUD-08) |
| T6 | Every object is signed and written once to the retention-locked bucket; a second write is refused; verification checks each signature and refuses a forged one | `TestAnchorsAreSignedAndWrittenOnce`, `TestClosesAreSignedAndAnchored` (a forged anchor refused), `TestVerifyIntegrityForARange` (CTL-AUD-02) |
| T7 | `audit.VerifyRange` and the nightly `VerifyAll` share the link check from the event before the range, and check every anchor, close anchor and detail fingerprint in it. Only a signed boundary or a signed recovery explains a gap, and verification checks its signed copy first | `TestVerifyIntegrityForARange` (an event changed in place fails at its sequence), `TestTheTrailIsPurgedBehindASignedBoundary`, `TestARestoreBehindTheAnchorsIsRecovered` (AUD-03, INV-12) |
| T8 | The database decides, not the job: the delete guards on `audit_event` and the ledger check the purge's flag, a boundary recorded in the same transaction, the switch, an age of at least 7 years and year order; the switch is a dual-control setting; only `fyniq_worker` executes the functions | `TestAttackThePurgeKeepsItsRules`, `TestAttackTheLedgerPurgeKeepsItsRules`, `TestTheAuditTrailsRetentionShipsAsDecided` (AUD-09, AUD-10, INV-02, INV-03) |
| T9 | The rule `purge-carry-forward@1` is pure and agrees with the independent oracle to the cent at every date from the boundary on; `ledger_purge` refuses to delete unless the carry-forward its transaction posted replaces the purged lines exactly; the job compares the balances cache with the lines before it commits | `TestThePurgeAgreesWithTheOracle`, `TestTheCarryForwardOfAYearByHand`, `TestTheLedgerIsPurgedAndEveryReportFromTheBoundaryKept`, `make differential` (AUD-10, QA-ENG-02, INV-07, INV-08) |
| T10 | The balance service refuses a date before the boundary, a purged period and a revision before `carried_revision` with `BALANCE-PURGED`; a record's page says when its postings were purged, and the screen lists each purge | `TestTheReadersAfterTheLedgerPurge`, `TestThePurgesAfterRetentionAreListed`, the `JournalPage` purge test (ENG-46) |
| T11 | The recovery step runs first in the nightly verification and in `jobs audit-recover`; it records only a chain that links and whose anchors' signatures verify, and leaves anything else to verification as tampering. It signs the recovery, starts a new epoch, notifies the Owners and DPOs at once, and fails the nightly job so the ops alert is raised | `TestARestoreBehindTheAnchorsIsRecovered`, `TestAPurgeThenARestoreBehindTheLastAnchor` (G4-071) |
| T12 | The recovery step re-applies every entry of the signed erasure register, checking each signature, before the copy opens (`docs/runbooks/audit-restore.md`) | `TestTheErasureRegisterIsReapplied` (G4-070) |
| T13 | `acknowledgeAuditRecovery` needs A on "Audit trail", which only the Owner may use (OwnerMay); a person signed in to Fyniq, never the AI; If-Match with the recovery's version; once (the unique row) | `TestARecoveryIsListedAndAcknowledgedByTheOwner`, `TestARecoveryFailsTheAuditCheckUntilAcknowledged`, `TestAuthorisationMatrix` |
| T14 | Every operation needs V on the row; the AI holds nothing on it and no MCP tool reads the trail; the export's download is the caller's only and is itself a security event | `TestTheAuditTrailExplorer` (a Bookkeeper refused), `TestAuthorisationMatrix`, the MCP red-team suite |
| T15 | A verification covers at most 366 days; an export at most 100,000 events, prepared by the worker; the explorer pages by cursor on indexed columns; every operation is rate-limited (FR-INT-16) | `TestVerifyIntegrityForARange` (a range too long refused), `TestTheAuditTrailExplorer` |

## 4. What remains

- **[live-verify] keys and the bucket in the cloud.** The worker and jobs accounts need `macSign` on the fingerprint
  key, or their proofs will not compare with the API's. The API needs to read the anchors' bucket and the signing
  key's public key. The bucket's retention lock is not set yet. Until the dev cloud checks these, only local mode
  proves T2, T6 and T12.
- **A real point-in-time restore** has not been run. The test simulates one, with an anchor published for events the
  database no longer holds.
- **Records cannot be held yet.** Legal holds exist only on stored files, so no hold stops a purge. When records can be
  held, both purges stop at the first one held.
- **Nobody holds A on "Audit trail".** The Owner acknowledges recoveries until Appendix D names a role (the design's
  open question).
- **The erasure register covers the security details only.** Per-person cohort keys and the data inventory's reason for
  the 400 days follow in `p1-pdpa-retention` (§11.8, CMP-PD-02).
- **Other records' pages** do not yet say when the purge removed their postings. Journals, bills and invoices do.

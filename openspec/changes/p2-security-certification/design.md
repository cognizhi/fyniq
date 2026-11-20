# Design

Decisions below were taken while the owner was away, on the recommended option, and are tagged `[owner to confirm]` here and in PRD-001's revision.

## D1. What a hold is (AUD-11)

A row of `retention_hold`: a kind, what it covers, a reason, who placed it and when, and, once lifted, who lifted it, when and why. Never
deleted and never changed except by being lifted once (a guard). Three kinds:

| Kind | Covers `[owner to confirm]` |
|---|---|
| `record` | One source document (an invoice, bill, journal, claim, ...: every one is a `document`): its audit events, the ledger entries posted from it, and its evidence files |
| `contact` | The contact's record and audit events; every document that names the contact (`customer_id`, `supplier_id` or `contact_id` in any version) with that document's events, entries and evidence; every ledger entry with a line for the contact (a director's or shareholder's account); not a draft that names nobody |
| `financial_year` | Every ledger entry dated in the year; every audit event that occurred in it (read on the UTC date widened by one day each side, so no time zone leaves an event out); the evidence of documents dated in it |

Why documents only for "a record": every posted record in Fyniq is a `document` with a version history, so one kind reaches all of them; the
payroll runs, bank statements and similar rows hang off the ledger entries they post. Not chosen: a hold on any table's row (needs a coverage rule per
table, and a purge that does not touch that table is no reason to hold it).

## D2. One truth for coverage: SQL functions

Coverage is defined once, in the database: `hold_covers_event(org, object_type, object_id, occurred_at)`, `hold_ledger_dates(org)` (the
posting dates of the covered entries; a year hold gives its first day) and `hold_evidence(org)`. Each begins with "no hold in force: false", is
`SECURITY DEFINER` with a fixed search path and filters on the organisation it is asked about, so a caller that cannot read `retention_hold`
fails closed to "held", never to "free".

- **The audit trail's purge** reads the covered sequence numbers of each batch it scans and stops its leading run before the first (a purge
  goes oldest first, so a hold keeps every later event too: a hold can only make the purge shorter).
- **The ledger's purge** passes `hold_ledger_dates` (those not before the previous boundary) to `retention.NextYear`, which already refuses a
  year with a held date in it.
- **The backstop.** `BEFORE DELETE` triggers on `audit_event` and `journal_entry` refuse a covered row (FY306); the `ledger_boundary` guard
  refuses a boundary after a covered date. Separate triggers: the existing purge functions are not re-declared, only tightened around.
- **Evidence files.** Evidence is never deleted, so the files need the storage-level hold (the cloud's retention lock expires with time, a hold
  does not). A reconcile (`retention.holds`, a platform job run after each change through the outbox and nightly) sets `SetLegalHold` on
  every covered final file, including files attached after the hold, and clears it only for a file no hold in force still covers.

## D3. Who, and what lifting means

The DPO or an Owner places and lifts, with a reason of 3 to 500 characters at both ends; each is an audit event (`retention_hold.placed`,
`retention_hold.lifted`) in the same transaction. A lift asks nobody else `[owner to confirm]`; it tells the Owners and the DPO in the app. Why: the
purge is off by default and dual-controlled when switched on, nothing goes within the 5-year minimum, and a second approver for each lift would
need the approval engine for a record that destroys nothing by itself. Not chosen: dual control on lifting (safer, an approval policy per lift);
the recommendation holds until the purge is switched on in a real organisation. A new Appendix D capability, `RetentionHolds`: Owner and DPO
C/A; Controller and Auditor V. Not on the AI or MCP surface.

A held contact is also refused by erasure and shredding (the PDPA allows keeping records for a legal purpose): a tightening, tested.

## D4. Security assessments (SEC-25)

`security_assessment` (kind `penetration_test`, `vulnerability_assessment` or `other`; the tester; whether independent; scope; dates; summary;
the report's SHA-256 and where it is kept), `security_finding` (reference, title, severity, found, due) and `security_finding_step` (fixed, retested,
or accepted by an Owner with a reason): all three are only ever added (FY362), so a finding's state is its last step. The report itself stays outside Fyniq `[owner to confirm]`: it describes how to break the system, and the register
needs only to show it exists and has not changed. Not chosen: attaching it as evidence on a document (the evidence store takes a document's
files, and a report is no document of the books).

- **Due dates** follow CTL-RES-04's SLA (Critical 7 days, High 30) and add Medium 90 and Low 180 `[owner to confirm]`, from the day found.
- **The control CTL-CHG-12** (the review `security-assessments`, read like the drills' reviews, the installation's own organisation only): signed
  while an independent assessment ended within 12 months and no Critical or High finding is open past its due day (or unretested 30 days after
  its fix); due from 30 days before the 12 months end; overdue after them and while a finding is overdue; with none recorded it waits
  without failing. A review maps to a control's pass or overdue only, so "fails" and "not assessed" of the first draft are overdue and
  pass-with-a-waiting-note, until the gate of `p3-saas-platform` makes the missing test fail. Before the first external customer it must pass: the gate arrives with `p3-saas-platform` (a planned check naming it).
- **Writes** are the Owner's (an Administrator would be an Owner's second hand on a row the Owner approves, which the Owner rule forbids without an approval-engine row); the register is read by the Administrator, Controller, Auditor and DPO. A finding moves
  forward only (open, fixed, retested; or accepted), enforced by a guard.
- The test itself and staging's nightly DAST need a firm and a cloud staging: `[live-verify]`.

## D5. Readiness by framework (CMP-08)

A read over the registry and the dashboard's statuses; nothing stored. Per framework (the keys of `compliance.Frameworks`), the stage of
PRD §14.8, controls by status and the controls that are not passing, in plain words. For ISO/IEC 27001:2022 Annex A it lists the clause
numbers (5.1 to 5.37, 6.1 to 6.8, 7.1 to 7.14, 8.1 to 8.34: 93) that no control's mapping names. Only the numbers are held in the repository:
the titles are the standard's text, not an official public page, so the statement of applicability's wording waits for the certification body
`[live-verify]`. Never a claim of certification; the page says readiness. Evidence export per framework stays CMP-04.

## D6. Surfaces

Holds: a tab on Compliance › Data protection (the DPO's page; the plan row says no new screen). Assessments: a tab on Compliance › Controls (the drills' neighbour). Readiness: a panel on Compliance › Controls. All
tagged Expert, with help keys, guide pages, knowledge pages and design fragments.

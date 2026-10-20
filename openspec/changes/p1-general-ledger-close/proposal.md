# Proposal

## Why

P0 built the posting engine and the manual journal as an API: a draft can be saved, posted and
reversed. Nobody can keep books with that yet. There are no periods unless the sample company is
loaded, so a founder's first journal is refused (FY030). Nothing lists journals or shows a ledger,
and there are no opening balances and no close. Every document of wave 2 (invoices, bills, claims,
assets) posts through the periods, the close and the scheduler this change builds. It is the first
change of wave 2 in the P1–P3 build plan (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`).

## What Changes

- **Periods and the financial year (FR-ORG-02, Appendix B "Period of a posting"):** monthly periods
  for a financial year, opened from the organisation's books start and first year end, listed with
  their status.
- **Manual journals in full (FR-GL-01, FR-GL-12, ENG-24):**
  - a list with filters;
  - drafts changed as new versions;
  - the preparer and the approver recorded;
  - the Accountant's limit and the approval engine;
  - an edit of a posted journal in an open period, posted as a reversal and a replacement.
- **Recurring and auto-reversing journals (FR-GL-02)**, created and posted by the scheduler.
- **Opening balances (FR-GL-06, posting rule "Opening balances")** at the books start date, netting
  to zero. INV-13 allows share capital in them.
- **The period close (FR-GL-07, FR-GL-08, FM-4, AUD-04):**
  - the close checklist;
  - the checks that must be clean;
  - the attestation by a founder who did not prepare the close;
  - the revision check of ENG-46/47 (G4-053);
  - the hard lock;
  - the close snapshot;
  - reopening through the approval engine, with a reason and before and after snapshots.
- **Year-end (FR-GL-10):** retained earnings computed without closing entries, and open items carried
  forward in one continuous ledger.
- **The general ledger report (FR-GL-11):** any accounts and any dates, with the running balance, the
  export and the drill to the source.
- **The scheduler and system postings (FR-AUT-01, FR-AUT-02, ENG-05):** a tick that runs what is
  due, under a named system principal, with period locks and approval policies applied.
- **Versioned posting rules (ENG-06):** a replay test for each rule version.
- **Validation (ENG-20, ENG-21, ENG-23):** the journal's ported statuses with their severities and
  messages.
- **The accounting framework and policies (FR-ORG-10)**, recorded with their approver and date.
- **Schedules (FR-GL-03)** for prepayments and deferred revenue, built with the first source that
  spreads over months (`p1-purchases` bills).
- **Flux analysis (FR-GL-14):** the month-on-month part comes with the close; the budget part waits
  for `p1-planning`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `ledger/general-ledger`: FR-GL-01, 02, 03, 06, 07, 08, 10, 11, 12 and 14;
- `automation/rules-and-schedules`: FR-AUT-01 and 02;
- `ledger/posting-engine`: ENG-05 and ENG-06;
- `ledger/posting-rules`: the posting conventions, manual journals, opening balances, edits of posted
  documents, system accounts and credit documents and opening items (the six unnumbered rows);
- `ledger/validation`: ENG-20, ENG-21 and ENG-23;
- `organisation/settings`: FR-ORG-10.

## Before it starts

The plan says a change starts once every change in its "After" column is done.
`p1-organisation-chart`, `p1-statutory-packs` and `p1-approval-engine` are not done. Under the
founders' "build first, sign off later" instruction (16 Oct 2026), this change starts now, **on the
parts of them it needs, which are built**:
- the chart, the profile's books start and first year end (`p1-organisation-chart` 2.1, 3.1);
- the approval engine's policies, envelopes and passkey-signed records (`p1-approval-engine` 1.x,
  2.1, 2.2), for reopening a period and for journals above a limit;
- the team modes and SOD-6 (`p1-approval-engine` 3.1).

What it does not need from them stays open there:
- the hierarchy and deactivation (`p1-organisation-chart` 3.2): the journal editor's account picker
  offers active leaf accounts as they are today;
- dimensions (3.3): journal lines take dimensions once 3.3 lands;
- the GST tax point (`p1-statutory-packs` 2.2): manual journals carry no GST code in P1.

Question 31 is decided as SFRS for Small Entities, if the accountant confirms the company qualifies.
That confirmation is still outstanding. This change records the framework as unconfirmed until then,
and keeps `[verify]` wherever the framework changes a figure.

## Impact

- **Code:**
  - the period, journal, opening-balance and close commands in `internal/domain`;
  - a periods API, journal list and versioning, opening balances, the close and its reopen, and the
    general ledger report;
  - the scheduler tick under the system principal;
  - migrations for close records, recurring journals and the INV-13 source rule.
- **Screens:**
  - journals and the journal editor;
  - opening-balances, period-close, schedules;
  - the general ledger report (its screen is in `p1-reports`; this change builds it early, because
    the founders need to see what they post).
- **Oracle:** new posting shapes (opening balances, auto-reversals, the replacement of an edited
  journal) widen the gap between the engine and the oracle. The oracle's author must not read the
  engine (QA-ENG-01), so each shape is a task for the oracle session. This change records it and does
  not write it.
- **Reviewer:** the accountant, for the close and the posting rules.

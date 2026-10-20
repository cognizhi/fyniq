# Design

## Context

- P0's posting engine is a pure function (ENG-01). `journal.Service` drafts, posts and reverses a
  manual journal, each command in one unit of work (ENG-02).
- The database assigns each entry its ledger revision and its period (ENG-46, INV-03). It refuses a
  posting date that no period covers (FY030) or that falls in a closed period.
- `storage.Periods` creates, closes and reopens periods under the ENG-47 lock order. Only
  `seedload` calls it today, so a new organisation has no periods.
- The organisation profile holds the books start and the first year end (FR-ORG-02).
- The approval engine signs envelopes. Its policies have steps and quorums, and amount conditions
  wait for the first object type that carries an amount: the journal (rev 0.40).
- Team modes and SOD-6 are built. FM-2's S$2,000 self-approval and FM-4's attestation are not.
- The trigger behind INV-13 refuses share capital only from the `manual_journal` source.

## Goals / Non-Goals

**Goals:**
- A founder can open the financial year, enter opening balances, keep journals, see the general
  ledger and close a month. Every step goes through the public API, and the database stays the
  backstop.

**Non-Goals:**
- Soft locks and per-module locks (FR-GL-09, P2).
- FX revaluation and rates (`p1-multi-currency-fx`).
- Intercompany journals (FR-GL-13, P3).
- The checks catalogue (`p1-checks`). The close runs the checks that exist when it runs.
- Opening open items (invoices and bills). They come with `p1-sales` and `p1-purchases`.

## Decisions

All of these are provisional, on the recommended option, until the accountant's sign-off.

- **Periods are calendar months, cut at the books start and at each year end.**
  - The first financial year runs from the books start to the first year end. Each later year is
    the twelve months after the one before.
  - A year that starts or ends mid-month gets a short first or last period.
  - A person opens a whole year at once: the first year, then each next one. Nothing opens a period
    by posting into it, so FY030 stays the backstop.
  - A year can be opened at most one year past the year that holds today's business date (DEV-05),
    so a typo cannot open 2099.
  - Once a period exists, the books start and the first year end no longer change. The profile
    refuses the change with a stable code.
  - Rights: C on "Period close / reopen" (the Accountant and the Controller).
  - Alternative: open each month as it starts. Rejected because a back-dated bill or an accrual
    then fails until someone opens the month.
- **Periods close in order, and only the latest closed period reopens.** That keeps one lock date,
  as the workbook and most ledgers have.
- **The close is two acts by two people (FR-GL-07, FM-4).**
  - The preparer (C on the row) runs the close checks. These record their result and the ledger
    revision they saw (ENG-46).
  - A second person (A on the row: the Owner or the Controller, not the preparer) attests and
    closes. The attestation records its own revision.
  - The close commits only if no entry dated on or before the period end has a revision later than
    the lowest recorded revision. Otherwise it fails "checks are stale, re-run" (G4-053). The
    database lock order of ENG-47 makes the race safe either way.
  - The FM-4 rule applies in Founder mode. Standard mode keeps the two people, as the Accountant
    may not close (§6.2).
- **The close checks, until `p1-checks` exists:**
  - ✖ every earlier period is closed;
  - ✖ the balances cache agrees with the lines (INV-08);
  - ✖ the trial balance up to the period end nets to zero;
  - ⚠ draft journals dated in the period, listed;
  - ⚠ opening balances not posted, for the first period.

  Reconciliations join with `p1-bank-import-reconcile`, and month-on-month flux (FR-GL-14) joins
  later in this change.
- **The close snapshot (AUD-04)** is the trial balance at the period end, per account and currency,
  as RFC 8785 canonical JSON with its SHA-256. It is stored on the close record and in the audit
  event. A close record never changes.
- **Reopening is an approval request (FR-GL-08).**
  - It is a new object type, `period`, with a reason.
  - The policy is one step with a quorum of one, someone other than the requester who holds A on
    the row. The quorum signs the envelope, as for every other type.
  - The approved request reopens the period. Its execution records the before snapshot (the
    close's) and the after snapshot (the trial balance at reopening).
- **Journals are listed from their documents.** A list filters by status, posting date and text,
  newest first, with a cursor.
  - A draft changes as a new version under If-Match.
  - The preparer is the person who saved the version that posted. The approver is the person who
    posted it, or the approval's signers.
- **Journal approvals (FR-GL-01, FM-2, SOD-3) are amount conditions on a `journal` policy.**
  - The amount is the journal's total debits in base currency.
  - In Founder mode the preparer posts at or below S$2,000 (Q5's setting): the post is flagged
    self-approved and logged for the FM-3 review. Above it, posting becomes an approval request
    signed by another person who holds A on Journals (the Controller).
  - In Standard mode SOD-3 makes the preparer of a journal above the threshold unable to approve it.
  - Voiding (reversing) always needs a second person in Founder mode (FM-1).
- **An edit of a posted journal** is allowed in an open period. It is a new version, and the post
  writes the reversal of the previous entry and the new entry in one transaction (FR-GL-12, posting
  rule "Edits of posted documents"). It needs the same approval as a void.
- **Opening balances are one document per organisation, source `opening_balance`.**
  - It is dated at the books start and holds base-currency lines.
  - It posts only when it nets to zero.
  - A change after posting is an edit under FR-GL-12, high-risk under FM-1.
- **INV-13 becomes an allow list.** The posting-source trigger accepts a line to a share-capital
  account only from `opening_balance` or `share_issue` (the register, wave 4). This replaces "refused
  from `manual_journal`". It is a tightening, so no other source can post to share capital by
  accident.
- **Year-end (FR-GL-10)** posts nothing. Retained earnings are computed as the profit and loss of
  every earlier year, in the reports and on the close page. The optional closing entry waits for
  the accountant (Q31).
- **The general ledger report** reads lines in base currency.
  - It is ordered by posting date, then ledger revision, then line number.
  - Each account has an opening balance (everything before the start date), its lines with a
    running balance, and a closing balance.
  - At most 5,000 lines per account per request. Beyond that, a narrower range is asked for,
    until the asynchronous export of `p1-reports`.
  - Rights: V on "Reports, dashboards" without a qualifier. A sales-only or purchases-only reader
    waits for the scoped reports.
- **The scheduler (FR-AUT-01, FR-AUT-02, ENG-05)** is one tick operation called by Cloud Scheduler
  through Cloud Tasks, and by `make dev`'s loop locally (ADR-T7).
  - Each due item runs once per slot, in `job_slot`.
  - It runs under the named system principal `system:scheduler`, with its own audit identity.
  - Period locks apply: a due posting in a closed period waits and is reported.
  - Approval policies apply as they would to a person.
- **Auto-reversing journals** carry a reversal date, by default the first day of the next period.
  The scheduler posts the reversal. It is part of the approved journal, so it needs no new approval.
- **Recurring journals** are a template with a monthly or yearly rule, a start and an end date.
  Each due date creates a draft from the template and tells the preparer through the outbox. The
  draft posts like any other journal.

## Ports and adapters (ADR-T8)

Storage, clock and queue. No new port.

## Audit events (AUD-01)

- year opened;
- close checks run;
- attested and closed;
- reopen requested, approved or executed;
- journal version saved, posted, reversed or edited;
- opening balances saved or posted;
- recurring template saved;
- each scheduled run and what it posted.

## Risks / Trade-offs

- [The oracle lags the engine on the new shapes] → Each shape is a task for the oracle session.
  `make test`'s fixed-seed sample never contains them, so it is not evidence of agreement.
- [A close that fails late frustrates the attesting founder] → The close page shows whether the
  checks are stale before the attestation is asked for.
- [5,000 lines per account per request is low for later years] → `p1-reports` adds the asynchronous
  export. The limit is a constant with a test.

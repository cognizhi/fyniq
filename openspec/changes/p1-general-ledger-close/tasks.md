# Tasks

## 1. Periods and the financial year

- [ ] 1.1 List periods and open a financial year from the books start and the first year end, as calendar months cut at the year ends; refuse a year more than one past today's, and changes to the books start once a period exists. Tests `// req: FR-ORG-02` and `// req: ledger/posting-rules Posting conventions` (the period of a posting)
- [ ] 1.2 The close checks with their result and the ledger revision they saw; the attestation and close by a second person; the revision check; the close snapshot. Tests `// req: FR-GL-07` (including G4-053 on two connections), `// req: FM-4` and `// req: AUD-04`
- [ ] 1.3 Reopening the latest closed period through the approval engine (object type `period`), with a reason and before and after snapshots; posting into a closed period refused by the database. Tests `// req: FR-GL-08`
- [ ] 1.4 Year-end: retained earnings as the profit and loss of every earlier year, without closing entries; open items carried forward. Test `// req: FR-GL-10` (the optional closing entry waits for question 31)

## 2. Journals

- [ ] 2.1 List journals by status, date and text with a cursor; save a draft as a new version under If-Match; record the preparer and the approver. Tests `// req: FR-GL-01`
- [ ] 2.2 Amount conditions on a `journal` approval policy: FM-2's self-approval at or below the threshold, flagged and logged for the FM-3 review; above it, an approval request; SOD-3 in Standard mode; FM-1 for voids. Tests `// req: FR-GL-01`, `// req: FM-2`, `// req: SOD-3` and `// req: FM-1`
- [ ] 2.3 Edit a posted journal in an open period: a new version whose post writes the reversal and the replacement in one transaction. Tests `// req: FR-GL-12` and `// req: ledger/posting-rules Edits of posted documents`
- [ ] 2.4 Auto-reversing journals (the reversal date, by default the first day of the next period) and recurring journal templates. Tests `// req: FR-GL-02`

## 3. Opening balances

- [ ] 3.1 One opening-balance document per organisation at the books start, in base currency, posting only when it nets to zero; INV-13 as an allow list (`opening_balance`, `share_issue`) in a new migration. Tests `// req: FR-GL-06`, `// req: ledger/posting-rules Opening balances` and a database attack test `// req: INV-13`

## 4. The general ledger report

- [ ] 4.1 Lines of any accounts and dates in base currency with opening, running and closing balances, at most 5,000 lines per account per request, and the source of each line. Tests `// req: FR-GL-11`

## 5. Scheduler and system postings

- [ ] 5.1 The scheduler tick: due items once per slot, under the `system:scheduler` principal, period locks and approval policies applied. Tests `// req: FR-AUT-01` and `// req: FR-AUT-02`
- [ ] 5.2 Recognition up to the current period only, with future amounts as projections. Test `// req: ENG-05`
- [ ] 5.3 Versioned posting rules with a replay test per rule version. Test `// req: ENG-06` (QA-ENG-06)

## 6. Validation, framework and schedules

- [ ] 6.1 The journal's ported workbook statuses with their severities and messages. Tests `// req: ENG-20`, `// req: ENG-21` and `// req: ENG-23`
- [ ] 6.2 The accounting framework and edition from a financial year, the accountant's confirmation and the policies with their approver and date. Tests `// req: FR-ORG-10`
- [ ] 6.3 Month-on-month flux on the close: variances above the threshold need an explanation before the close is signed off. Test `// req: FR-GL-14` (the budget half waits for `p1-planning`)
- [ ] 6.4 Schedules for prepayments and deferred revenue, with the first source that spreads over months (`p1-purchases` bills). Tests `// req: FR-GL-03`

## 7. Screens

- [ ] 7.1 Build journals, the journal editor and the journal record page as designed. Add help keys, tooltips and `docs/user-guide/` pages compiled into `kb/`
- [ ] 7.2 Build opening-balances and period-close as designed, with the periods of the year on the close page
- [ ] 7.3 Build the general ledger report (its screen belongs to `p1-reports`; built early so the founders see what they post)
- [ ] 7.4 Build schedules with 6.4

## 8. The oracle (for the oracle session; never written from the engine)

- [ ] 8.1 Opening balances, auto-reversals and the reversal and replacement of an edited journal as scenario shapes, generator kinds and golden cases, written from the PRD rules by an author who has not read the engine (QA-ENG-01)

## 9. Finish

- [ ] 9.1 Every new operation in the contract (`make openapi`), with its authorisation-matrix rows
- [ ] 9.2 Finish with `make check`, `make test` and `make test-e2e`

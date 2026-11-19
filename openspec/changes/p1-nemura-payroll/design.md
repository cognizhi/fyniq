# Design

## Context

PRD-001 §8.7 and Appendix B.3 are the Singapore payroll rules; ADR-N2 has Nemura implement them. The statutory values are already researched packs that both apps read:
`sg-cpf` (rates by group, age and PR year, the ordinary and additional wage ceilings, graduated low wages), `sg-sdl` and `sg-shg`, with accessors in the shared pure
pack library (`CPFRatesOn`, `CPFCeiling`, `CPFLowWagesOn`, `CPFOWMax`, `SDLOn`, `SHGOn`). Fyniq has no payroll engine; its side (the projection, verifying and posting a run,
salary payments, holds) is `p1-payroll-link`. Nemura has people, classification, leave, the leave inputs to pay (unpaid leave, encashment, overtime: `p1-nemura-leave-time` D8),
pay terms (`person_terms`), letters, passkeys, an approval primitive, the outbox and a renderer port. The shared Go libraries Nemura may import are `money`, `rounding`, `pack`,
`audit` and the infrastructure (archtest); the Fyniq packages are not.

## Goals / Non-Goals

**Goals:** a pay run whose every figure traces to its inputs and its rules and agrees with an independent calculation to the cent; segregation (nobody approves the run they
prepared); an audit trail without pay in it; payslips people can trust; corrections that are visible versions.
**Non-Goals:** the CPF file, IR8A, AIS (`p2-payroll-outputs`); Fyniq's posting and the bank (`p1-payroll-link`); the link's events (`p1-nemura-fyniq-link`); a provider's connector;
the oracle's Python half (the oracle session); filing or paying anyone (`[live-verify]`); the foreign worker levy's table (O2).

## Decisions

- **D1. The employee-month is the unit.** The engine takes one person's wages for one calendar month (every run and correction of it together) and returns the whole month's
  figures: wage classes, ordinary and additional wages for CPF, CPF (total, employee, employer), SDL, self-help group fund, net pay, the deductions and the amount to disburse.
  A run is the set of its people's employee-months; a later run or correction of the month recomputes it and the difference is what changes (B.3). The engine is pure: no
  clock, storage or network; the pack version and the year to date are inputs; it returns the working (each step with its source) so a payslip and the approval can show it.
- **D2. Money is exact, rounding is named.** Amounts are `apd` decimals with SGD's scale, never floats; the database holds `NUMERIC(20,4)` with scale at most 2; JSON carries
  decimal strings. `RND-CPF` (total = round half up of (capped OW + AW) x (ER + EE rates), employee share rounded down to the dollar, employer = total − employee),
  `RND-CPF-GRAD` (the S$50 to S$750 band's formulas, then the dollar rounding) and `RND-SDL` (per employee clamp(round(wages x rate, 2), min, max); the employer-month total
  rounded down to the dollar, the sub-dollar difference is not payable: decision 27, recommended default, the accountant confirms) become pure functions in the shared
  `internal/domain/rounding`, each named where it is used, with the PRD's hand-worked cases pinned. Nothing in the engine rounds except through a named policy.
- **D3. Wage classes follow facts, not names.** A pay item carries the employment period it is for, the date it is payable, its nature and whether it is CPF wages (FR-PAY-17);
  it is ordinary wages when it is for the month's employment and payable by the 14th of the following month, additional wages otherwise; a name only suggests a default that the
  run checks, and a mismatch is a ⚠ the preparer resolves. IR8A, SDL and payslip classes are kept apart from the CPF class.
- **D4. The year to date makes the additional wage ceiling work.** Per person and calendar year, for this employer: ordinary wages subject to CPF and additional wages subject
  to CPF to date, imported at cut-over for a company that starts mid-year (a missing figure is ✖ on the first run that pays an additional wage: FR-PAY-14). An additional
  wage paid before the year's last pay month uses the estimated ordinary wages of the year; December or the last month of employment recalculates with the actual and adds a
  shortfall to that month or raises a refund task for an excess, the employee's share repaid to the employee. The permanent-resident start date and a joint election are
  effective-dated with evidence; the age band changes from the first day of the month after the birthday. Values come from the pack only.
- **D5. A run has states, and a version for every change.** Draft, calculated, approved, posted, paid in part, paid, closed (NM-PAY-10); a calculated run is read-only, a change
  is a new version of the run that states the full corrected figures of its wage month and names the version it supersedes (NM-PAY-09), never an edit. Every employee line keeps
  its inputs, rules and pack version, so the version can be re-derived. A run's rows are append-only; the database refuses an update of an approved version and a delete.
- **D6. Segregation is in the database.** Whoever prepared a run cannot approve it (SOD-4, NM-ROLE-04); the approver confirms with a passkey over the run's digest (the approvals
  primitive and the envelope the link uses, `p1-nemura-fyniq-link`); in a team of one the Owner is the second person. A check constraint and a trigger refuse the same member as
  preparer and approver, and an approval that names another digest. An override needs a reason and the approval of the run, and is flagged ⚠ on the approval screen.
- **D7. Deductions are checked apart from the journal.** The kinds MOM allows, the 50% limit on the salary payable in the period (not counting absence, recovery of advances, loans,
  overpaid salary or unearned benefits, or co-operative society payments, and not on termination), advances recovered over at most 12 months and at most 25% of the period's salary,
  written consent that can be withdrawn (FR-PAY-16). Each deduction links to its source and is used once. What the rule leaves open for final pay stays an accountant question
  `[accountant to confirm]`; the engine stops at the limits MOM states and says which rule it applied.
- **D8. Pay is restricted by role and never in an event.** Appendix B: Payroll administrator and Owner read and write pay, nobody else; the HR team sees that a run exists, not
  amounts. The audit event of a run names the run, its version, its totals' digest and its state, never an amount or a person's pay; no log or error carries pay; a person
  reads their own payslip only. A datacat category and a DPIA row cover it, and the API refuses by role before it reads.
- **D9. Inputs link to their source and are used once.** Joiners and leavers (the person's start and last day, prorated by the pack's rule), pay changes effective in the month
  (`person_terms` and a pay history), unpaid leave and encashment (`leave_ledger`, `p1-nemura-leave-time` D8), approved overtime and rest-day pay for Part IV employees
  (`overtime`), one-off allowances and bonuses, and claims paid through payroll when the company chooses. Each input row names its source row; the run marks it used, so a second
  run cannot use it again, and a correction reuses what its version used.
- **D10. The calendar and the late checks.** Pay day per pay group, the cut-off for inputs and the latest approval date; salary within 7 days of the end of the salary period and
  overtime within 14 days (`sg-people` `timing`); Checks warns of a run late against these (N.1 #2), books not received (#6), a rejected, returned or unknown payment (#14) and
  final pay due (#15). The checks read run and payment states; they never move money.
- **D11. Payslips are generated for the person, and delivered by Nemura.** The twelve MOM items from the employee-month's working, in a PDF the renderer draws and a print view,
  published to My pay when the salary is paid or within 3 working days (NM-PAY-04); a leaver reads theirs through the narrow account (NM-ESS-06). A payslip is a document with a
  hash like a letter; it is generated once per run version and never edited.
- **D12. Standalone hand-off is a file with a manifest.** A journal file (CSV: department, account, debit, credit) and a salary payment file (CSV: instruction, payee reference,
  amount) with a manifest (run, version, totals, SHA-256), built from the approved run; sealed bank details come from the restricted store. The file's format is generic: a bank's
  own GIRO format is a connector's job `[live-verify]`. "Paid" comes only from a person recording evidence or, linked, from the bank's events; an exported file is never paid.
- **D13. The oracle is the reviewer, not the author.** The Go engine is written from PRD-001 B.3 and the packs; the independent Python oracle (the oracle session) is written from the
  same rules and compared by the differential run on generated employee-months. This change adds the hand-worked cases (golden) and the scenario schema the oracle reads;
  `make differential` proves agreement once the oracle's half exists. Until then the tasks that need it are marked.

## Owner decisions

Decided 20 Nov 2026 under the owner's standing instruction (the owner is away: take the recommended option for small and medium choices, record why). Anything touching a statutory
value stays tagged for confirmation.

### O1. Where the engine lives

| Option | Pros | Cons |
|---|---|---|
| **A. `internal/nemura/payroll`, pure, with the rounding policies added to the shared `internal/domain/rounding`** | Nemura stays in its lane (ADR-N4, NM-ARC-05); the policies are named once and the oracle mirrors them; no Fyniq code is imported | A small widening of one shared pure package |
| B. A package shared with Fyniq | One engine | Couples the apps; Fyniq has no engine to share; breaks the archtest rule |

**Decided: A.**

### O2. The foreign worker levy (FR-PAY-05, priority S)

| Option | Pros | Cons |
|---|---|---|
| **A. An expense input entered per person and month by the Payroll administrator; no levy table in P1** | Meets "recorded as an expense where applicable"; no unverified statutory table | The levy is not calculated; the payroll administrator enters what MOM bills |
| B. A levy pack by pass type, sector and skill tier | Calculated | Needs MOM's current tiers and the company's sector and quota facts, which Nemura does not hold `[verify]` |

**Decided: A.** The levy is the employer's cost, not an employee deduction and not CPF wages; B follows when a source and the facts exist.

### O3. Standalone files

| Option | Pros | Cons |
|---|---|---|
| **A. Generic CSV journal and payment files with a manifest and a digest** | Testable offline; no bank format guessed | A bank's own format needs a connector |
| B. A named bank's GIRO format | Directly usable | A format we cannot verify offline `[live-verify]` |

**Decided: A.**

### O4. Pay data in Nemura's tables

| Option | Pros | Cons |
|---|---|---|
| **A. Pay lines in ordinary columns behind forced RLS and the payroll role, with no pay in any audit event, log or outbox row** | Queryable for the run; the same protection as other restricted tables | A database administrator reads it |
| B. Seal every amount with the key service | Opaque at rest | The run's arithmetic, ordering and sums cannot be done in the database; per-row key calls on every run |

**Decided: A,** as for the pay terms (slice 2 of `p1-nemura-self-service-letters`); bank account numbers and NRIC or FIN stay sealed.

## Risks / Trade-offs

- [A rounding policy applied at the wrong place] -> each policy is one named pure function with the PRD's hand-worked cases pinned, and the oracle compares generated months.
- [A run approved by its preparer] -> refused by a database constraint, not only by the API.
- [Pay in a log, an event or an error] -> a redaction test over the audit events, the outbox and the error catalogue; the API reads after the role check.
- [A pack value out of date in January] -> the pack's own review routine; the run records the pack version it used on every line.
- [A correction that posts twice] -> versions carry the superseded version; Fyniq posts the difference (`p1-payroll-link` D7); Nemura never sends a total twice under one version.

## Migration plan

Additive migrations from the next number (check the directory: the link agent shares the sequence); no data to migrate. The payroll feature is off until an organisation switches it on.

## Decisions made while building the payslips (slice 4)

- **O6 Publishing is a step after approval.** The Payroll administrator or the Owner publishes the payslips of an approved version with one action, once for each version; a corrected version has its own and the first is marked corrected. Why: it is what the person sees, so it is deliberate; the deadline check (NM-PAY-10 payment states, check N.1 #9) arrives with the payment states (slice 5), because the clock runs from the confirmed payment.
- **O7 The payslip is data and a PDF.** The twelve items are stored as JSON beside the PDF the renderer draws from them, and the PDF's SHA-256 is checked on every read. Why: MOM's twelve items are the pack's list (`sg-people@2026.7` `payslip_items`); the data is what the page shows and the PDF is the document issued. The release build has no renderer yet, so publishing answers NM_UNAVAILABLE there (`[live-verify]` production renderer).
- **O8 Ad-hoc and fixed items.** Allowances are the fixed pay items and the one-offs whose nature is `allowance`; other one-offs (bonus, commission) are additional payments; the employee's CPF and the self-help group fund are deductions with the fixed and ad-hoc ones and no-pay leave. A payslip whose items do not add up to the net is refused, never rounded.
- **O9 A leaver reads theirs through the narrow account** (NM-ESS-06), and nobody else's: another login finds no such payslip.

## Decisions made while building the payment states (slice 5a)

- **O10 One table for payment states, written by two paths.** `nemura.pay_payment_event` (migration 000031) holds append-only events keyed on the run and the person, not on a version, so an unknown outcome still blocks after a corrected version. Fyniq's status events (the link's inbox, `p1-nemura-fyniq-link` 2.3) and a person's record both go through `RecordPaymentState`; the vocabulary is PRD-002 NM-PAY-10's and is documented for the link in `contracts/peoplelink/README.md`. Why: the link agent confirmed no other table or vocabulary exists; one table keeps "unknown blocks" a single rule.
- **O11 The database holds the legal moves.** A trigger allows only the moves of `payroll.LegalMove` (a test compares all 64 pairs against the database); the first event of a payment may be any state, because a person recording an outcome standalone has no earlier ones; an unknown outcome resolves only to paid, rejected or returned.
- **O12 Evidence is a document of the person paid.** A manual record needs `recorded_by` and an `evidence_id` that is a document of that person (a bank statement line, a transfer receipt), by a database constraint and trigger, not only by the API. Why: the coordinator's rule that "paid" is never a bare word; the existing document store already keeps hashed, access-controlled evidence.
- **O13 The payslip's date of payment is the confirmed one.** When a payment is already confirmed paid, the payslip carries that date; before, the pay group's scheduled pay day. Checks (#9) counts the 3 working days from the confirmed date and warns when a payslip's date differs; the correction is the next version's payslip. A payslip is not rewritten.
- **O14 Item 11 (overtime payment period) stays empty.** Only overtime worked in the wage month is paid in that month's run (the run reads entries by work date), so the overtime period is the salary period and the item is not needed (MOM: "if different").
- **Left, recorded `[verify]`:** the people pack's holiday table has the holiday dates but not the Monday "in lieu" days (1 Jun, 10 Aug, 9 Nov 2026; 8 Feb 2027), which MOM's page says apply when the holiday falls on the person's rest day; a working-day count (Checks #4, #9, leave) may be one day short in those weeks until the pack models the rest-day rule. My pay does not yet show a person's own payment state (the NM-PAY-10 scenario); the payslip is the person's record for now.

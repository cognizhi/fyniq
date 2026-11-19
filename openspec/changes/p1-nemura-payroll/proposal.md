# Proposal

## Why

Nemura holds people, their pay terms, leave and the letters that state them. It cannot yet pay anyone. Payroll is where a mistake costs real money and a statutory
breach at once: a CPF amount rounded the wrong way, an additional wage that ignores the annual ceiling, a deduction beyond what MOM allows, a pay run approved by the
person who prepared it, a figure posted twice. PRD-002 (ADR-N2) puts the payroll rules in Nemura and keeps them in PRD-001 §8.7 and Appendix B.3; Fyniq posts and pays what
Nemura calculates (`p1-payroll-link`). This change builds the calculation, the pay run and what the people it pays receive: exact decimals, the rounding policies by name,
the employee-month as the unit, every override and correction a version, and a payslip for each person. The independent Python oracle's half belongs to the oracle session;
this change pins hand-worked cases that the oracle must agree with.

Timing (PRD-002 question 1 and 12, decided 16 Oct 2026): built after Fyniq's go-live and before the first paid person; a payroll provider bridges any hire before then.
The payroll feature stays off for an organisation until it is switched on (NM-FEAT-01), and it needs Leave (NM-FEAT-02).

## What Changes

- **The engine** (FR-PAY-01 to 04, 14, 17, 18; Appendix B.3; `RND-CPF`, `RND-CPF-GRAD`, `RND-SDL`): a pure, exact-decimal calculation of the employee-month from wage
  items, with wage classes by CPF Board's facts, CPF by residency, age band and permanent-resident year from the pack, the ordinary and additional wage ceilings and the
  year to date, SDL per employee and per employer-month, self-help group funds from the pack, net pay, deductions and the amount to disburse.
- **Pay records and inputs** (FR-EMP-01, FR-PAY-05, NM-PAY-02): effective-dated pay terms, allowances and deductions per person, pay groups and the payroll calendar
  (NM-PAY-03), and the inputs a run collects from leave, overtime and joiners and leavers, each linked to its source.
- **The pay run** (FR-PAY-01, 06, 07, 16; NM-PAY-09, 10, 11): draft, calculated, approved (preparer and approver are different people, the approver confirms with a
  passkey), posted, paid in part, paid, closed; overrides with a reason and an approval; corrections as new versions that state the full corrected month; salary
  deduction limits; what changed since last month.
- **What people receive** (NM-PAY-04, FR-PAY-08 for Nemura): itemised payslips with the twelve MOM items in My pay, with a print and a PDF view, and the leaver's final pay.
- **Hand-off** (NM-PAY-05): the approved run as a summary journal and a salary payment file with a manifest and a digest when Nemura stands alone; the contract's
  events when linked are `p1-nemura-fyniq-link`'s. A file is never proof of payment.
- **Screens:** `nm-payroll-hr`, `nm-payrun-approval`, `nm-my-pay`, `nm-payslip`, `nm-checks` (the payroll checks).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `people/payroll` (NM-PAY-01 to 11), `payroll/pay-runs` and `payroll/employees` (the rows
Nemura implements), and the rounding policies of `ledger/money-and-rounding`.

## Impact

- **Code:** `internal/nemura/payroll` (the pure engine, wage classes, deductions, the run's states); `internal/adapters/postgres/nemura`; `db/nemura/migrations/` from the
  next number (the migration sequence is shared with the link agent: check the directory before adding one); `contracts/openapi/nemura/`; `apps/nemura-web`; the
  shared `internal/domain/rounding` gains `RND-CPF`, `RND-CPF-GRAD` and `RND-SDL` as pure functions; a Nemura pack version only when a rule needs a value no pack holds.
- **Not here:** the CPF submission file, IR8A and the Auto-Inclusion Scheme (`p2-payroll-outputs`); the link's events and the posting in Fyniq (`p1-payroll-link`,
  `p1-nemura-fyniq-link`); a payroll provider's connector (`p1-nemura-assistant-external`); anything that files with, or pays, a real authority or bank (`[live-verify]`).
- **Security and privacy:** pay is the most restricted data Nemura holds (Appendix B: Pay, pay history, pay runs: Payroll administrator, Owner). Pay never enters an audit
  event, a log, an outbox payload or an error message; a run is read and written only with the payroll right; the oracle and the engine agree to the cent.
- **Needs a second reviewer** (money movement, approvals and segregation, payroll postings, the pack values), whoever wrote the code: waived before production by the
  founders on 14 Oct 2026, so the change is held for `needs-human-review` when the pull request is opened.

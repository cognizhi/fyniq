# Tasks

Each numbered group is one slice, in this order, and each ends with the local gates green: `make check`, the Go tests of the touched packages in both flavours,
`make api-check`, `make db-lint`, the web typecheck and tests. A slice never leaves a test red or a requirement tagged that its tests do not fully prove.
`[live-verify]` marks what only the cloud, a bank or an authority can show. `[oracle]` marks what waits for the oracle session's Python half.

Load before each slice: `fyniq-accounting-domain` (money, rounding, postings), `fyniq-sg-statutory` (1, 2, 5), `fyniq-feature-change`, `fyniq-api-integrations`,
`fyniq-security-compliance` (3, 4, 6: pay data, approvals, segregation), `fyniq-design-system` and `fyniq-knowledge-base` (every screen), `fyniq-quality-gates`,
`fyniq-release` (a PRD revision).

## 1. The engine (slice 1)

- [ ] 1.1 Pure functions `RND-CPF`, `RND-CPF-GRAD` and `RND-SDL` in `internal/domain/rounding`, named, with the PRD's hand-worked cases and edge cases (49.99 and 50.00, 750.00 and
  750.01, 8,000 ceiling, a dollar boundary for the employee share, SDL minimum and maximum, the employer-month total rounded down). `// req: RND-CPF, RND-CPF-GRAD, RND-SDL`
- [ ] 1.2 Wage classes by facts (FR-PAY-17): an item is ordinary or additional wages by the period it is for and the date it is payable; the name only suggests a default; a mismatch is
  reported. Tested on the PRD's examples (monthly commission payable by the 14th, quarterly commission, an annual bonus). `// req: FR-PAY-17`
- [ ] 1.3 The employee-month (B.3): gross, ordinary wages for CPF with the ceiling, additional wages with the annual ceiling and the year to date, CPF by residency, age band (from the
  first day of the month after the birthday) and PR year from the pack, the December or last-month true-up with a shortfall or a refund task, SDL per employee and per employer-month,
  the self-help group fund with opt-out, net pay. Returns the working with each step's source. `// req: FR-PAY-01, FR-PAY-02, FR-PAY-03, FR-PAY-04, FR-PAY-14`
- [ ] 1.3a A later run or correction of the month recomputes the whole employee-month and returns the difference from what was posted. `// req: FR-PAY-01`
- [ ] 1.4 Deductions and what a run pays out (FR-PAY-16, FR-PAY-18): the allowed kinds, the 50% limit with its exceptions, advances over at most 12 months and 25% a period, consent;
  reimbursements and held monies; the amount to disburse and the totals of B.3's postings balancing (gross + employer CPF + SDL + reimbursements = advances + other deductions + 2100 + 2110).
  `// req: FR-PAY-16, FR-PAY-18`
- [ ] 1.5 The scenario schema and the golden employee-months (`test/differential/`) the oracle reads; a property test that nothing rounds outside a named policy and that the totals balance
  for generated months. `[oracle]` for the agreement run. `// req: QA-ENG-02`
- [ ] 1.6 Verify: the local gates; the foreign worker levy is an expense input (O2), not a calculation.

## 2. Pay records, inputs and the calendar (slice 2)

- [ ] 2.1 Migration: pay groups and the payroll calendar (pay day, cut-off, latest approval), the pay items of a person (basic, allowances, fixed deductions, with the facts of FR-PAY-17),
  the pay history (effective-dated, only ended), the self-help group fund and opt-out, the permanent-resident election with evidence, and the year to date (imported at cut-over, a missing
  figure ✖). Forced RLS, role grants for pay only, audit without amounts. `// req: FR-EMP-01, NM-PAY-03`
- [ ] 2.2 Inputs for a run (NM-PAY-02): joiners and leavers prorated, pay changes in the month, unpaid leave and encashment from the ledger, approved overtime and rest-day pay, one-off
  allowances and bonuses, claims paid through payroll; each links to its source and is used once. `// req: NM-PAY-02`
- [ ] 2.3 API, screens `nm-payroll-hr` (pay terms and inputs), guide and kb pages; the data inventory and DPIA rows for pay. `// req: FR-PAY-13`
- [ ] 2.4 Verify.

## 3. The pay run (slice 3)

- [ ] 3.1 Migration and domain: the run, its versions and lines (append-only), the states (draft, calculated, approved, posted, paid in part, paid, closed), calculation from the inputs with
  the pack version on every line, the working kept. `// req: FR-PAY-01, NM-PAY-10`
- [ ] 3.2 Overrides with a reason and an approval, flagged ⚠; corrections as new versions that state the full corrected month and name what they supersede, the difference computed.
  `// req: FR-PAY-06, NM-PAY-09`
- [ ] 3.3 Approval: the preparer cannot approve (a database constraint and trigger), the approver confirms with a passkey over the run's digest, the Owner is the second person in a team of one.
  The attack tests: the preparer approving, an approval over another digest, an approved version edited, a run read without the payroll role. `// req: FR-PAY-07, NM-ROLE-04`
- [ ] 3.4 What changed since last month (NM-PAY-11) and the approval screen `nm-payrun-approval`, with the ⚠ items and unexplained changes. `// req: NM-PAY-11`
- [ ] 3.5 Verify, including the redaction test: no audit event, log, outbox row or error carries pay.

## 4. Payslips and My pay (slice 4)

- [ ] 4.1 The itemised payslip, the twelve MOM items from the employee-month's working, drawn by the renderer port and hashed, once per run version. `// req: NM-PAY-04`
- [ ] 4.2 My pay (`nm-my-pay`, `nm-payslip`): by month, view, print, PDF; published when the salary is paid or within 3 working days; the leaver's narrow account reads theirs. `// req: NM-PAY-04, NM-ESS-02`
- [ ] 4.3 Guide and kb pages; verify.

## 5. Final pay, payment states and the checks (slice 5)

- [ ] 5.1 Final pay (NM-PAY-06, NM-PAY-08): the last salary, leave encashment or deduction from the settlement of `p1-nemura-self-service-letters` 4.2, notice pay, the deadline by case, the IR21
  clearance hold as a flag the payment path reads. `// req: NM-PAY-06, NM-PAY-08`
- [ ] 5.2 Payment states per person (scheduled to returned or unknown), recorded with evidence when standalone; an unknown outcome blocks a new payment for that person and run. `// req: NM-PAY-10`
- [ ] 5.3 The payroll checks of N.1: late against the calendar (#2), books not received (#6), a rejected, returned or unknown payment (#14), final pay due (#15). `// req: NM-PAY-03`
- [ ] 5.4 Verify.

## 6. Hand-off files (slice 6)

- [ ] 6.1 The standalone journal file and salary payment file with a manifest and a digest (O3), built from the approved run and the sealed bank details; never proof of payment. `// req: NM-PAY-05`
- [ ] 6.2 Verify the whole change: `make check`, `make test` offline, the differential run once the oracle's half exists `[oracle]`, the performance of a 100-person run, and the second reviewer's list.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-PAY-01 The rules are PRD-001's | 1.1 to 1.5 | | the oracle's agreement `[oracle]` |
| NM-PAY-02 Inputs | 2.2 | | |
| NM-PAY-03 Payroll calendar | 2.1, 5.3 | | |
| NM-PAY-04 Payslips to self-service | 4.1, 4.2 | | access after leaving is NM-ESS-06 |
| NM-PAY-05 Hand-off to the books | 6.1 | the standalone files | the link's events: `p1-nemura-fyniq-link` |
| NM-PAY-06 Final pay | 5.1 | | IR21 filing: `p2-payroll-outputs` |
| NM-PAY-07 Delivery map | 1 to 6 | | |
| NM-PAY-08 Final pay deadlines | 5.1, 5.3 | | the retrenchment notification to MOM `[verify]` |
| NM-PAY-09 Corrections | 3.2 | | Fyniq posts the difference: `p1-payroll-link` |
| NM-PAY-10 Run and payment states | 3.1, 5.2 | | the bank's events: the link |
| NM-PAY-11 What changed | 3.4 | | |
| FR-PAY-01 to 04, 14, 17, 18 | 1.2 to 1.4 | | |
| FR-PAY-05 Foreign worker levy | 2.1 | an expense input (O2) | the levy table |
| FR-PAY-06, 07 | 3.2, 3.3 | | |
| FR-PAY-13 Payroll data restricted | 2.3, 3.5 | | |
| FR-PAY-16 Salary deductions | 1.4 | | final pay on termination `[accountant to confirm]` |
| FR-EMP-01, FR-EMP-02 | 2.1 | the Nemura side | the department to account mapping: `p1-payroll-link` D8 |
| RND-CPF, RND-CPF-GRAD, RND-SDL | 1.1 | | the oracle's copy `[oracle]` |

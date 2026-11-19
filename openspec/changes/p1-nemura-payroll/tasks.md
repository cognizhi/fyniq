# Tasks

Each numbered group is one slice, in this order, and each ends with the local gates green: `make check`, the Go tests of the touched packages in both flavours,
`make api-check`, `make db-lint`, the web typecheck and tests. A slice never leaves a test red or a requirement tagged that its tests do not fully prove.
`[live-verify]` marks what only the cloud, a bank or an authority can show. `[oracle]` marks what waits for the oracle session's Python half.

Load before each slice: `fyniq-accounting-domain` (money, rounding, postings), `fyniq-sg-statutory` (1, 2, 5), `fyniq-feature-change`, `fyniq-api-integrations`,
`fyniq-security-compliance` (3, 4, 6: pay data, approvals, segregation), `fyniq-design-system` and `fyniq-knowledge-base` (every screen), `fyniq-quality-gates`,
`fyniq-release` (a PRD revision).

## 1. The engine (slice 1)

- [x] 1.1 Pure functions `RND-CPF`, `RND-CPF-GRAD` and `RND-SDL` in `internal/domain/rounding`, named, with the PRD's hand-worked cases and edge cases (49.99 and 50.00, 750.00 and
  750.01, 8,000 ceiling, a dollar boundary for the employee share, SDL minimum and maximum, the employer-month total rounded down). `// req: RND-CPF, RND-CPF-GRAD, RND-SDL`
- [x] 1.2 Wage classes by facts (FR-PAY-17): an item is ordinary or additional wages by the period it is for and the date it is payable; the name only suggests a default; a mismatch is
  reported. Tested on the PRD's examples (monthly commission payable by the 14th, quarterly commission, an annual bonus). `// req: FR-PAY-17`
- [x] 1.3 The employee-month (B.3): gross, ordinary wages for CPF with the ceiling, additional wages with the annual ceiling and the year to date, CPF by residency, age band (from the
  first day of the month after the birthday) and PR year from the pack, the December or last-month true-up with a shortfall or a refund task, SDL per employee and per employer-month,
  the self-help group fund with opt-out, net pay. Returns the working with each step's source. `// req: FR-PAY-01, FR-PAY-02, FR-PAY-03, FR-PAY-04, FR-PAY-14`
- [x] 1.3a A later run or correction of the month recomputes the whole employee-month and returns the difference from what was posted. `// req: FR-PAY-01`
- [x] 1.4 Deductions and what a run pays out (FR-PAY-16, FR-PAY-18): the allowed kinds, the 50% limit with its exceptions, advances over at most 12 months and 25% a period, consent;
  reimbursements and held monies; the amount to disburse and the totals of B.3's postings balancing (gross + employer CPF + SDL + reimbursements = advances + other deductions + 2100 + 2110).
  `// req: FR-PAY-16, FR-PAY-18`
- [x] 1.5 The scenario schema and the golden employee-months (`test/differential/`) the oracle reads; a property test that nothing rounds outside a named policy and that the totals balance
  for generated months. The agreement run: the oracle's half (`oracle/src/oracle/employee_month.py`, written from the rules) and the differential gate (`internal/nemura/payroll/differential`,
  `make differential` with `payroll-month`). `// req: QA-ENG-02`
  Built (oracle session, 20 Nov 2026): the employee-month and run in exact decimals; `fyniq-payroll-run@1`; 9 golden runs; the gate and the Hypothesis search agree to the cent on every figure of the month,
  the run's totals and B.3's entry. It found three engine defects (the wage band on wages paid, advances not kept apart for 1250, no refund in the graduated range), fixed. **Left:** the rules the sources leave
  open, decided in the oracle and marked `[verify]` in `test/differential/README.md` (the year's estimated ordinary wages, the joint election as F/G, SHG and SDL on the gross, final-pay advances `[accountant to confirm]`);
  overrides (FR-PAY-06) are not in the differential.
  Second review (20 Nov 2026): the rules were re-decided from the CPF Board, MOM and SkillsFuture pages (`test/differential/README.md`, "Payroll months and runs" with each source's URL and quote);
  a refund's employee share is whole dollars on a whole-dollar total (engine fixed, `fix(engine)`), F/F is built in the engine and oracle, and overrides, ratio amounts (part month, unpaid leave, overtime)
  and final-pay advances are in the differential with golden runs. **Left:** recording F/F (`nemura.pr_election` has no kind: a migration, the API and the screen); NM-PAY-12 counts calendar days
  for a part month where MOM's page counts working days (owner decision O5); SDL and SHG on the gross, 29 February and the refund total's rounding `[accountant to confirm]`.
- [x] 1.6 Verify: the local gates; the foreign worker levy is an expense input (O2), not a calculation.
  Built in slice 1: `internal/domain/rounding/payroll.go`; `internal/nemura/payroll` (`Classify`, `Calculate`, `CheckDeductions`, `Total`, `Postings`, `Diff`); Nemura's copies of `sg-cpf`, `sg-sdl` and
  `sg-shg` (`internal/nemura/packs.StatutoryFiles`, kept equal to Fyniq's by an archtest); `test/differential/payroll.schema.json` and 16 hand-worked golden months, with a property test of a thousand
  generated months. Left in slice 1: the CPF Board's treatment of a birthday on the first of a month `[verify]`; the SPR joint election is read from the person (its
  evidence is slice 2); the engine does not yet read the salary of a part month (joiners and leavers are inputs prorated in slice 2).

## 2. Pay records, inputs and the calendar (slice 2)

- [x] 2.1 Migration 000028: pay groups and the payroll calendar (pay day, cut-off, latest approval), the pay items of a person (basic, allowances, fixed deductions, with the facts of FR-PAY-17),
  the pay history (effective-dated, only ended), the self-help group fund and opt-out, the permanent-resident election with evidence, and the year to date (imported at cut-over, a missing
  figure ✖). Forced RLS, role grants for pay only, audit without amounts. `// req: FR-EMP-01, NM-PAY-03`
- [ ] 2.2 Inputs for a run (NM-PAY-02): joiners and leavers prorated, pay changes in the month, unpaid leave and encashment from the ledger, approved overtime and rest-day pay, one-off
  allowances and bonuses, claims paid through payroll; each links to its source and is used once. The one-off items are built (2.1); the collection of the rest into a run's items is built
  with the run, in 3.1, where it is used and tested end to end. `// req: NM-PAY-02`
- [x] 2.3 API (pay groups, a person's pay, the joint election, the year to date, one-off items; the Payroll administrator and the Owner alone), screen `nm-payroll-hr`, guide and kb pages; the
  data inventory row for pay. `// req: FR-PAY-13`
  Left in 2.3: the DPIA row for pay (with the run's, in 3.5); hourly and daily paid workers (monthly pay only in P1); KETs reading the pay from this record instead of the terms.
- [x] 2.4 Verify: the local gates.

## 3. The pay run (slice 3)

- [x] 3.1 Migration 000029 and domain: the run, its versions and lines (append-only), the states (draft, calculated, approved, posted, paid in part, paid, closed), calculation from the inputs with
  the pack version on every line, the working kept. `// req: FR-PAY-01, NM-PAY-10`
- [x] 3.2 Overrides with a reason, flagged ⚠, part of the digest; corrections as new versions that state the full corrected month and name what they supersede. `// req: FR-PAY-06, NM-PAY-09`
  Built in 3.1 and 3.2: `CalculateRun` collects the pay record (part months by RND-PAY-RATIO, NM-PAY-12), one-off items, unpaid leave and approved overtime, each linked to its source and marked used
  only at approval (3.3); the year to date is the import plus the latest approved line of each month; a missing fact refuses the run naming the person. Left: the approval and "what changed since
  last month" (3.3, 3.4); encashment of leave (its CPF class is `[verify]`), claims paid through payroll, the IR21 hold and off-cycle runs; rest-day and public-holiday overtime pay (a one-off item);
  a fixed deduction "for another purpose" needs a consent record that does not exist yet, so it fails ✖ until one does.
- [x] 3.3 Approval: the preparer cannot approve (a database constraint and trigger), the approver confirms with a passkey over the run's digest, the Owner is the second person in a team of one.
  The attack tests: the preparer approving, an approval over another digest, an approved version edited, a run read without the payroll role. `// req: FR-PAY-07, NM-ROLE-04`
  Built: the approver is checked by the API (Payroll administrator or Owner, fresh passkey) and the preparer is refused by the adapter and by a constraint; approval supersedes older approved
  versions, marks the inputs used once and writes the passkey's envelope; the approval page `nm-payrun-approval`.
- [x] 3.4 What changed since last month (NM-PAY-11) and the approval screen `nm-payrun-approval`, with the ⚠ items and unexplained changes. `// req: NM-PAY-11`
- [x] 3.5 Verify, including the redaction test: no audit event, log, outbox row or error carries pay.

## 4. Payslips and My pay (slice 4)

- [x] 4.1 The itemised payslip, the twelve MOM items from the employee-month's working, drawn by the renderer port and hashed, once per run version. `// req: NM-PAY-04`
- [x] 4.2 My pay (`nm-my-pay`, `nm-payslip`): by month, view, print, PDF; published when the salary is paid or within 3 working days; the leaver's narrow account reads theirs. `// req: NM-PAY-04, NM-ESS-02`
- [x] 4.3 Guide and kb pages; verify.

## 5. Final pay, payment states and the checks (slice 5)

- [x] 5.1 Final pay (NM-PAY-06, NM-PAY-08): the last salary, leave encashment or deduction from the settlement of `p1-nemura-self-service-letters` 4.2, notice pay, the deadline by case, the IR21
  clearance hold as a flag the payment path reads. `// req: NM-PAY-06, NM-PAY-08`
- [x] 5.2 Payment states per person (scheduled to returned or unknown), recorded with evidence when standalone; an unknown outcome blocks a new payment for that person and run. `// req: NM-PAY-10`
- [x] 5.3 The payroll checks of N.1: payslip late (#9), a rejected, returned or unknown payment (#14), late against the calendar (#2) and final pay due (#15) are built; books not received (#6) needs Fyniq's `payroll.run.posted` event and arrives with `p1-nemura-fyniq-link`. `// req: NM-PAY-03`
- [ ] 5.4 Verify.

## 6. Hand-off files (slice 6)

- [x] 6.1 The standalone journal file and salary payment file with a manifest and a digest (O3), built from the approved run and the sealed bank details; never proof of payment. `// req: NM-PAY-05`
- [x] 6.2 Verify the whole change: `make check`, the Nemura suites offline (the adapter's database tests in Testcontainers, the engine, the pack and the web tests), a pay run of 100 people calculates in 1.2 s on the laptop (NM-NFR-02 asks 5 s for 50; `TestAPayRunOfAHundredPeopleCalculatesInUnderTenSeconds`). **Left:** the Python oracle's half of the differential run `[oracle]` (another agent is writing it from the PRD without reading the engine; a disagreement is fixed in the engine with a failing test first); a bank's own file format and the production renderer `[live-verify]`; the DPIA of Nemura's pay processing is for the DPO (`fyniq-security-compliance`; `compliance/nemura-data-inventory.yaml` has the row); CPF class of leave pay and pay in lieu of notice, a retrenchment's deadline and the contract's notice period `[accountant to confirm]`/`[verify]`.
  **For the second human reviewer** (money movement, approvals, the audit trail, tenancy; AGENTS.md "Safety"): (1) approval segregation and the 5-minute passkey (`pay_run_version_segregation`, `Session.Fresh`); (2) the payment-state trigger's legal moves and the evidence rule (migration 000031); (3) the tax-clearance hold as written-once versions and its release evidence (000032); (4) the bank-account opening for the payment file: audited read, fresh passkey, no account or amount in any audit event or log (000033, `exports.go`); (5) the payslip's append-only hash and the refusal to serve changed bytes (000030); (6) the grants and schema goldens (`go test -tags local -run 'TestEachRoleHoldsExactlyItsGrants|TestSchemaSnapshot' ./internal/adapters/postgres/nemura`) show no write role beyond `nemura_api` INSERT of the new tables; (7) the statutory facts and their `[verify]` tags in `sg-people@2026.7` to `2026.9`.

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

## Left (from the oracle session's review, 20 Nov 2026)

The independent oracle found four disagreements with the engine, which were fixed in the engine with a failing test first (see `git log internal/nemura/payroll`: the refund's whole-dollar employee share, the F/F joint election, and the two aligned on the second review), and recorded what the sources leave open in `test/differential/README.md`. What stays, each tagged:

- **Overrides (FR-PAY-06)** are in the engine and its database tests, but the oracle's generator does not draw them: the differential run does not prove an overridden figure `[oracle]`.
- **The F/F joint election** is recorded now (migration 000039, `kind` fg or ff, `TestAFirstYearResidentsElectionKindDecidesWhichRatesTheRunUses`): the engine reads it, the differential scenario `joint-application-at-full-rates` covers the engine and the oracle, and S$5,000 of wages is S$250 of employee CPF at F/G and S$1,000 at F/F `[verify: the CPF Board's treatment]`.
- **Part-month pay (NM-PAY-12):** the PRD counts calendar days of each pay record over the days of the month, MOM's page counts working days. Reported, not changed `[accountant to confirm]`; the method is a named constant (`payroll.PartMonthMethod`) with hand-worked goldens of the adapter's day counts (`TestAPartMonthIsCountedByCalendarDays`), which the differential run does not draw.
- **Final-pay advances (FR-PAY-16):** the engine lifts the 50% limit in the final month and recovers an advance only as far as consented, but there is no record of an advance and its consent yet: `other_deduction` consent `[accountant to confirm]`.
- **Wage class of pay for the month before payable by the 14th**, **SDL and SHG on the gross**, **a 29 February birthday on 1 March**, **the graduated band on the wages subject to CPF** and **a first-of-month birthday**: decided in the oracle from the CPF Board, MOM and SkillsFuture pages of 20 Nov 2026 and tagged `[accountant to confirm]` or `[verify]`.
- **The foreign worker levy** is an expense input (design O2), not calculated.
- **The DPIA of Nemura's pay processing** is written for the DPO (`compliance/nemura-dpias/pay.md`; kept apart from `compliance/dpias/` because Fyniq's DPIA registry lists that directory exactly).
- **Mutation coverage of the engine (QA-ENG-04):** `internal/nemura/payroll` is in `MUTATION_PKGS`: 205 of 214 viable mutants killed (efficacy 95.79%, gremlins 0.6.0, 20 Nov 2026) after `boundary_test.go` pinned every limit of a record at its edge, the CPF age at each birthday, the comparison's order and causes, the payslip's headings and the payment moves. Nine survivors are equivalent or unreachable (two comparisons whose branches return equal values, the sort's tie, a panic message); the lines the tests cannot reach are the engine's own arithmetic-error panics and the `differential` runner, which the oracle gate covers.

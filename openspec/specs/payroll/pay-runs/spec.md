# payroll/pay-runs Specification

## Purpose
How Fyniq runs Singapore payroll each month: gross to net, CPF, SDL, SHG and the foreign worker levy, overrides and year-to-date state, approval, posting and payment, payslips and statutory files, the IR21 clearance hold, the go-live gate, salary-deduction limits and the restriction of payroll data, checked against hand-pinned results. Derived from PRD-001 §8.7 and §G.2.

## Requirements

### Requirement: FR-PAY-01 Monthly pay runs
The system SHALL run pay runs per month for employees from the register, computing gross pay, employee CPF, employer CPF, SDL, SHG, net pay and total cost for each employee.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Employees come from the register
- **WHEN** a pay run is created for a month
- **THEN** its employees come from the employee register

#### Scenario: Gross pay components
- **WHEN** an employee's gross pay is computed in a pay run
- **THEN** it is built from salary, allowances, overtime, bonus as additional wages, deductions and unpaid leave

#### Scenario: Statutory amounts, net pay and total cost
- **WHEN** a pay run is computed
- **THEN** each employee has employee CPF, employer CPF, SDL, SHG, net pay and total cost

### Requirement: FR-PAY-02 CPF contributions
The system SHALL compute CPF by residency, PR year and age band from effective-dated tables, with the Ordinary and Additional Wage ceilings, graduated rates and CPF Board rounding [re-verify every January].

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Rates come from the effective-dated table
- **WHEN** CPF is computed for an employee-month
- **THEN** the rates come from the table effective for that month, by the employee's residency, PR year and age band

#### Scenario: Age band changes the month after the birthday
- **GIVEN** an employee whose birthday moves them into a new CPF age band
- **WHEN** CPF is computed for the month of the birthday and the month after
- **THEN** the new age band applies from the first day of the month after the birthday

#### Scenario: Ordinary Wage ceiling
- **GIVEN** ordinary wages above S$8,000 in a month
- **WHEN** CPF is computed
- **THEN** CPF on ordinary wages is computed on the Ordinary Wage ceiling of S$8,000 a month

#### Scenario: Additional Wage ceiling tracked year to date
- **WHEN** additional wages are paid
- **THEN** the additional wages subject to CPF are limited to the Additional Wage ceiling: the S$102,000 annual salary ceiling minus the ordinary wages subject to CPF in the year, tracked year to date

#### Scenario: Graduated rates for low wages
- **GIVEN** an employee's wages for the month are from S$50 to S$750
- **WHEN** CPF is computed
- **THEN** the graduated rates for that wage band apply

#### Scenario: First- and second-year PR graduated rates
- **GIVEN** a permanent resident in the first or second year of PR
- **WHEN** CPF is computed
- **THEN** the first- or second-year PR graduated rates apply

#### Scenario: CPF Board rounding
- **WHEN** an employee's CPF is computed
- **THEN** the total contribution is rounded to the nearest dollar, with 50 cents rounding up
- **AND** the employee share is rounded down
- **AND** the employer share is the total minus the employee share

#### Scenario: G4-003 Same CPF in one run or two runs
- **GIVEN** a citizen aged 55 or under earns S$3,005 in a month
- **WHEN** it is paid as one run, and again as two runs of S$1,502.50
- **THEN** both give total 1,112, EE 601, ER 511 (not 600 / 512)

### Requirement: FR-PAY-03 Skills Development Levy
The system SHALL compute SDL on total remuneration, with a minimum and maximum per employee per calendar month, for all employees including foreign employees, and an SDL payable per employer-month (`RND-SDL`).

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: All pay runs of the month together
- **GIVEN** an employee is paid in more than one pay run in a calendar month
- **WHEN** SDL is computed
- **THEN** the minimum and maximum apply to the employee's total remuneration from all pay runs of that calendar month together

#### Scenario: Foreign employees pay SDL
- **WHEN** a pay run includes foreign employees
- **THEN** SDL is computed for them as for every other employee

#### Scenario: SDL payable rounded down once per employer-month
- **WHEN** the SDL payable for an employer-month is computed
- **THEN** it is the total of the employee amounts for the month, rounded down to the dollar (`RND-SDL`)

#### Scenario: SDL payable in the CPF submission and on 2110
- **WHEN** the CPF submission for the month is prepared and 2110 CPF & SDL payable is cleared
- **THEN** the SDL in the submission is the SDL payable
- **AND** 2110 must clear to the SDL payable

#### Scenario: Organisation with only foreign employees
- **GIVEN** an organisation whose employees are all foreign employees
- **WHEN** its SDL is paid
- **THEN** it is paid directly to the agency CPF Board names [verify]

#### Scenario: G4-002 Off-cycle bonus in the same month
- **GIVEN** the G.2 March employees in one run
- **WHEN** a S$4,000 off-cycle bonus is paid in the same month to the employee earning S$4,000
- **THEN** that employee's SDL is recomputed on the month's S$8,000 and is S$11.25, not 10.00 + 10.00
- **AND** the employer-month total becomes S$72.00, so the payable goes from S$70 to **S$72**, rounded once, and the off-cycle run posts S$2 (per-run calculation would give S$80)
- **AND** 2110 clears to the submission and the cash paid

### Requirement: FR-PAY-04 Self-help group fund deductions
The system SHALL deduct self-help group (SHG) fund contributions (CDAC, ECF, MBMF, SINDA) by wage band from tables, and SHALL record an employee's opt-out.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Deduction by wage band
- **GIVEN** an employee whose SHG fund is CDAC, ECF, MBMF or SINDA
- **WHEN** a pay run is computed
- **THEN** the SHG deduction is the amount the fund's table gives for the employee's wage band

#### Scenario: Opt-out recorded
- **WHEN** an employee opts out of their SHG fund
- **THEN** the opt-out is recorded on the employee
- **AND** the pay run makes no SHG deduction for that employee

### Requirement: FR-PAY-05 Foreign worker levy
The system SHALL record the foreign worker levy as an expense where it applies.

Source: PRD-001 §8.7 · Priority S · Phase P1

#### Scenario: Levy recorded as an expense
- **GIVEN** a foreign worker levy applies to an employee for a month
- **WHEN** that month's payroll is recorded
- **THEN** the levy is recorded as an expense

### Requirement: FR-PAY-06 Pay-run overrides
The system SHALL allow overrides of computed pay-run amounts only with a reason and an approval, and SHALL flag every override ⚠ (warning).

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Override needs a reason and an approval
- **WHEN** a user overrides a computed pay-run amount without a reason or without an approval
- **THEN** the override does not take effect

#### Scenario: Override is flagged
- **GIVEN** an override with a reason and an approval
- **WHEN** the pay run is shown
- **THEN** the overridden amount is used
- **AND** it is flagged ⚠

### Requirement: FR-PAY-14 Year-to-date and life-cycle state per employee
The system SHALL keep year-to-date and life-cycle state per employee: OW and AW subject to CPF from 1 January for this employer, the SPR start date (PR year), any joint election to contribute at full rates, and every override (FR-PAY-06) and correction as part of the employee-month (Appendix B.3).

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: AW ceiling per employee, per employer, per calendar year
- **WHEN** the Additional Wage ceiling is applied
- **THEN** it uses the OW and AW subject to CPF from 1 January for this employer
- **AND** the ceiling is per employee, per employer, per calendar year

#### Scenario: Year-to-date imported at cut-over
- **GIVEN** cut-over (M3, FR-MIG-01) in a financial year that starts after 1 January
- **WHEN** employees are migrated
- **THEN** their OW and AW subject to CPF are imported from 1 January

#### Scenario: Missing year-to-date blocks the first AW run
- **GIVEN** an employee's year-to-date is missing
- **WHEN** the first pay run that pays them AW is computed
- **THEN** it is a blocking error (✖)

#### Scenario: AW before the last pay month uses estimated OW
- **WHEN** AW is paid before the year's last pay month
- **THEN** the AW ceiling uses the estimated OW for the year

#### Scenario: Recalculation with actual OW
- **WHEN** December or the employee's last month of employment, whichever is earlier, is run
- **THEN** CPF on AW is recalculated with the actual OW and any shortfall is added to that month
- **AND** any excess raises a refund task, with the employee's share repaid to the employee [verify against the CPF Board AW guidance]

#### Scenario: SPR start date and full-rate election are effective-dated
- **WHEN** an employee's SPR start date (PR year) or a joint election to contribute at full rates is recorded
- **THEN** it is effective-dated and carries its evidence [verify]

#### Scenario: Overrides and corrections belong to the employee-month
- **WHEN** an override (FR-PAY-06) or a correction is made for an employee
- **THEN** it is kept as part of that employee-month (Appendix B.3)

#### Scenario: G4-005 Cut-over in July with a December bonus
- **GIVEN** cut-over on 1 July with January–June pay history
- **WHEN** a December bonus is paid
- **THEN** the AW ceiling uses January–December OW subject to CPF for this employer
- **AND** the result equals a full-year run

### Requirement: FR-PAY-07 Pay run approval and posting
The system SHALL post a pay run after its approval (SOD-4): salary expense by department account and dimensions, employer CPF and SDL, with the liabilities to 2100 Salaries payable and 2110 CPF & SDL payable (workbook rule), where 2110 also holds the SHG deductions paid to the CPF Board.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Approval under segregation of duties
- **WHEN** a pay run is approved
- **THEN** the approval follows SOD-4

#### Scenario: Posting an approved pay run
- **WHEN** an approved pay run posts
- **THEN** salary expense posts to each department's account with its dimensions
- **AND** employer CPF and SDL post as expenses
- **AND** the liabilities post to 2100 Salaries payable and 2110 CPF & SDL payable

### Requirement: FR-PAY-08 Itemised payslips and key employment terms
The system SHALL produce itemised payslips with the 12 MOM items and SHALL generate key employment terms (KETs).

Source: PRD-001 §8.7 · Priority M · Phase P2

#### Scenario: Payslip with the salary payment
- **WHEN** salary is paid
- **THEN** the itemised payslip with the 12 MOM items is given with the salary payment or within 3 working days of it

#### Scenario: Payslip on termination or dismissal
- **WHEN** employment ends by termination or dismissal
- **THEN** the payslip is given together with the outstanding salary (MOM)

#### Scenario: Payslip delivery
- **WHEN** a payslip is given
- **THEN** it is published to the MFA-protected self-service area or sent as an expiring link
- **AND** it is never sent as an NRIC-password PDF

#### Scenario: KETs generator
- **WHEN** key employment terms are needed for an employee
- **THEN** the KETs generator produces them

### Requirement: FR-PAY-09 CPF submission file
The system SHALL produce a CPF submission file for EZPay / One-Stop Payroll, as there is no public CPF API.

Source: PRD-001 §8.7 · Priority S · Phase P2

#### Scenario: Submission file for the month
- **WHEN** the month's pay runs are approved
- **THEN** a CPF submission file for EZPay / One-Stop Payroll can be produced for that month

#### Scenario: CPF due date
- **WHEN** a month's CPF is due
- **THEN** it is due at month end
- **AND** it must be paid by the 14th of the next month to avoid interest

### Requirement: FR-PAY-10 IR8A, Auto-Inclusion Scheme and IR21
The system SHALL produce the IR8A / Auto-Inclusion Scheme file (Appendix 8A/8B where relevant) and run an IR21 workflow with a clearance hold on all monies due to the employee on every payment path.

Source: PRD-001 §8.7 · Priority M from 5 employees; else S · Phase P2 / P1 (IR21 hold)

#### Scenario: Auto-Inclusion Scheme file
- **WHEN** the year's employment income is filed
- **THEN** the IR8A / Auto-Inclusion Scheme file, with Appendix 8A/8B where relevant, is due 1 March
- **AND** it is mandatory from 5 employees, directors included

#### Scenario: IR21 applicability
- **WHEN** an employee ceases employment, is posted overseas or leaves Singapore for more than 3 months
- **THEN** IR21 applies to non-citizen employees per IRAS
- **AND** to SPRs only when leaving permanently or posted overseas [verify]
- **AND** an exemption is recorded with its reason

#### Scenario: IR21 filing deadline
- **WHEN** IR21 applies
- **THEN** the IR21 is filed at least 1 month before the date of cessation, posting or departure

#### Scenario: Clearance hold on every payment path
- **GIVEN** an employee to whom IR21 applies
- **WHEN** the organisation knows of the cessation (normally the resignation date)
- **THEN** a clearance hold starts that day on all monies due to the employee (salary, bonus, overtime, leave pay, allowances, reimbursements, gratuities, lump sums)
- **AND** it holds them on every payment path: pay runs, payment batches, claims (FR-AP-17), advances and director balances [verify the last with the tax agent]

#### Scenario: Release of the hold
- **WHEN** IRAS's notification to release monies is recorded, or its directive has been paid
- **THEN** the held monies are released, and not before

#### Scenario: Hold applies from P1
- **WHEN** an employee to whom IR21 applies is paid, from phase P1 onwards
- **THEN** the clearance hold applies (FR-PAY-15)

#### Scenario: G4-007 Employment Pass holder resigns
- **GIVEN** an Employment Pass holder resigns on 3 March, leaving on 3 April, with an approved S$420 claim, an unsettled S$800 advance and March salary
- **WHEN** the resignation is recorded
- **THEN** the IR21 task is due by 3 March
- **AND** from 3 March the claim, the salary and any batch are held (C.2 #44), and nothing is paid until IRAS's notification
- **AND** recovering the advance from final pay is checked against FR-PAY-16 (termination case [verify with the accountant])

#### Scenario: Hold with Nemura linked
- **GIVEN** Nemura is linked
- **WHEN** a payment to an employee is released in Fyniq
- **THEN** the hold lives in Nemura and Fyniq checks it at that release (FR-PPL-13)

### Requirement: FR-PAY-11 Salary payment batch
The system SHALL pay salaries through a salary payment batch (FR-BNK-12).

Source: PRD-001 §8.7 · Priority S · Phase P1

#### Scenario: Net pay goes into a payment batch
- **WHEN** an approved pay run's salaries are paid
- **THEN** the net pay of its employees goes into a salary payment batch (FR-BNK-12)

### Requirement: FR-PAY-15 Payroll go-live gate
A pay run that pays an employee SHALL NOT be approved for payment (FR-PAY-07, FR-PAY-11) until the organisation has recorded, as evidence (FR-DOC-05), how that month's statutory outputs are met.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Arrangements the gate requires
- **WHEN** a pay run that pays an employee is approved for payment
- **THEN** the recorded evidence must cover itemised payslips (FR-PAY-08, a named provider, or a documented manual process), KETs for new employees, CPF/SDL/SHG submission and payment by the due date (FR-PAY-09 or manual CPF EZPay entry) and, where they apply, AIS and IR21 (FR-PAY-10)

#### Scenario: Missing arrangement blocks approval
- **GIVEN** one of the required arrangements is not recorded
- **WHEN** the pay run is approved for payment
- **THEN** it is a blocking error (✖)

#### Scenario: Director on no salary
- **GIVEN** a director on "No salary" (FR-EQ-04)
- **WHEN** the organisation's payroll is checked
- **THEN** that director does not trigger the gate (open question 28)

#### Scenario: G4-009 No recorded payslip arrangement
- **GIVEN** no payslip arrangement is recorded
- **WHEN** a pay run is approved for payment
- **THEN** it is a blocking error (✖) at approval

### Requirement: FR-PAY-16 Salary deduction limits
For employees covered by the Employment Act [verify coverage], the pay run SHALL check salary deductions, separately from the journal, against the kinds MOM allows, the 50% limit and the limits on recovering advances, and SHALL name the rule a breach breaks.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Only the kinds MOM allows
- **WHEN** a pay run has a salary deduction
- **THEN** it must be one of: absence; damage or loss; accommodation, amenities and services; recovery of advances, loans, overpaid salary or unearned benefits; CPF; income tax as IRAS's agent; court orders; any other purpose with the employee's written consent

#### Scenario: Written consent can be withdrawn
- **GIVEN** a deduction made with the employee's written consent
- **WHEN** the employee withdraws the consent, at any time
- **THEN** the deduction is no longer allowed
- **AND** the consent is kept as evidence (FR-DOC-05)

#### Scenario: 50% limit
- **WHEN** the deductions in a salary period are totalled
- **THEN** they are at most **50%** of the salary payable in the period, not counting absence, recovery of advances, loans, overpaid salary or unearned benefits, or co-operative society payments
- **AND** the limit does not apply on termination

#### Scenario: Advance open at termination
- **GIVEN** an employee leaves with an advance not yet recovered
- **WHEN** the final pay run is computed, while it is not settled whether the advance limits apply to final pay [verify with the accountant, G4-007]
- **THEN** the pay run recovers no more of the advance from final pay than the employee has consented to in writing
- **AND** it shows the open advance for a person to decide

#### Scenario: Recovery of advances
- **WHEN** an advance is recovered from salary
- **THEN** it is recovered in instalments over at most **12 months**
- **AND** each instalment is at most **25%** of that period's salary

#### Scenario: Breach blocks the pay run
- **WHEN** a deduction breaks one of these rules
- **THEN** it is a blocking error (✖) with the rule named

#### Scenario: G4-008 Advance recovered in two instalments
- **GIVEN** an advance of S$3,000 on a S$4,000 salary
- **WHEN** it is recovered in two instalments
- **THEN** it is a blocking error (✖): an instalment of S$1,500 is over 25% of S$4,000
- **AND** recovery in three instalments of S$1,000 passes

### Requirement: FR-PAY-17 Wage classes by facts, not names
Each pay item SHALL carry its employment period, payable date, nature and CPF applicability, and SHALL be ordinary wages only when it is for the month's employment and payable by the 14th of the next month, additional wages otherwise; a name or type SHALL only suggest a default.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Same name, different facts
- **GIVEN** two pay items both called "Commission": one for October's sales payable on 28 Oct, one for the year's sales payable next March
- **WHEN** the October and March runs classify them
- **THEN** the first is ordinary wages and the second additional wages
- **AND** the employee-month CPF and the year to date agree with the oracle (FR-PAY-14)

#### Scenario: Classes kept apart
- **WHEN** a pay item is classified
- **THEN** its tax (IR8A), SDL and payslip classes are kept apart from its CPF class

#### Scenario: Commission paid less often than monthly
- **WHEN** commission is paid less often than monthly
- **THEN** its CPF class follows CPF Board's guidance on commission [verify]

### Requirement: FR-PAY-18 What a run pays out
A run SHALL keep separate totals for gross wages, contributions, deductions authorised under FR-PAY-16, non-wage reimbursements and monies held under FR-PAY-10, and SHALL compute each person's amount to disburse as net pay + reimbursements − held monies, each source item used once.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Advance recovery and a booked claim in one run
- **GIVEN** an approved S$300 advance recovery and a S$100 claim already booked to 2150, both in the October run
- **WHEN** the run is posted
- **THEN** the person's disbursement falls by S$300 and rises by S$100
- **AND** 1250 is cleared by S$300 and 2150 by S$100, each exactly once
- **AND** wage expense is unchanged and the S$100 is not CPF wages

#### Scenario: Held monies
- **GIVEN** a person under a tax-clearance hold
- **WHEN** the run is calculated
- **THEN** their held monies stay in 2100, flagged, and are not in the amount to disburse

#### Scenario: Oracle
- **WHEN** a run's totals are computed
- **THEN** the oracle computes the same totals to the cent (Appendix B.3)

### Requirement: FR-PAY-13 Payroll data is restricted
Payroll data, including data derived from it, SHALL be visible only to Payroll administrators and Owners by default.

Source: PRD-001 §8.7 · Priority M · Phase P1

#### Scenario: Default visibility
- **WHEN** a user who is neither a Payroll administrator nor an Owner opens payroll data
- **THEN** it is not shown to them
- **AND** Payroll administrators and Owners see it

#### Scenario: Per-person cost rates need payroll rights
- **GIVEN** a user without payroll rights
- **WHEN** they view project costs
- **THEN** they see no per-person cost rate (FR-PRJ-06)
- **AND** they see project cost totals

#### Scenario: Pay-run journal lines carry no per-person detail
- **WHEN** a pay run posts
- **THEN** its journal lines carry no per-person detail

#### Scenario: Small groups are flagged
- **GIVEN** a department or dimension value whose payroll cost covers fewer than 3 people (setting)
- **WHEN** Checks runs
- **THEN** it shows a warning (⚠), a DPIA item
- **AND** it offers to post those people's payroll cost to a combined value

#### Scenario: AI access to payroll
- **WHEN** the AI is asked for payroll data
- **THEN** it answers only if the organisation setting explicitly allows AI access to payroll and the user has the payroll role (§9.4)

#### Scenario: G4-066 One-person department
- **GIVEN** a department has one employee
- **WHEN** a Bookkeeper opens the P&L by department and the project cost rates, and the Owner reads that employee's AI conversation log
- **THEN** Checks warns about the one-person group
- **AND** the Bookkeeper sees no per-person cost rate
- **AND** AI log entries with payroll data are masked for roles without payroll rights

#### Scenario: Salary payment lines
- **GIVEN** a salary payment batch in Fyniq
- **WHEN** a user without the salary-payments right opens it
- **THEN** they see the batch total, the number of people and the release state, not the lines (FR-PPL-17)

### Requirement: Hand-pinned payroll results
The pay run SHALL reproduce the hand-pinned payroll results of Appendix G.2 (2026 CPF rates, from `tests/test_edge_posting.py`); the SDL figures are per employee, before the employer-month rounding (`RND-SDL`), and net is gross − employee CPF − SHG.

Source: PRD-001 §G.2

#### Scenario: Employee aged 55 or under in March
- **GIVEN** 2026 CPF rates
- **WHEN** an employee aged ≤ 55 is paid gross 7,600.00 in March
- **THEN** EE CPF is 1,520, ER CPF 1,292, SDL 11.25, SHG 3.00 (CDAC, wage above S$7,500) and net 6,077

#### Scenario: Employee aged 55 to 60 in March
- **GIVEN** 2026 CPF rates
- **WHEN** an employee aged 55–60, opted out of SHG, is paid gross 5,000.00 in March
- **THEN** EE CPF is 900, ER CPF 800, SDL 11.25 and net 4,100

#### Scenario: PR aged 60 to 65 in March
- **GIVEN** 2026 CPF rates
- **WHEN** a PR aged 60–65 from the third year of PR status (full rates; first- and second-year rates are graduated, ENG-30 [verify]), opted out of SHG, is paid gross 4,000.00 in March
- **THEN** EE CPF is 500, ER CPF 500, SDL 10.00 and net 3,500

#### Scenario: Employee aged 65 to 70 in March
- **GIVEN** 2026 CPF rates
- **WHEN** an employee aged 65–70, opted out of SHG, is paid gross 3,000.00 in March
- **THEN** EE CPF is 225, ER CPF 270, SDL 7.50 and net 2,775

#### Scenario: Employee aged over 70 in March
- **GIVEN** 2026 CPF rates
- **WHEN** an employee aged > 70, opted out of SHG, is paid gross 2,500.00 in March
- **THEN** EE CPF is 125, ER CPF 188, SDL 6.25 and net 2,375

#### Scenario: Foreigner in March
- **GIVEN** 2026 CPF rates
- **WHEN** a foreigner is paid gross 9,000.00 in March
- **THEN** EE CPF is 0, ER CPF 0, SDL 11.25, no SHG and net 9,000

#### Scenario: OW capped at 8,000 plus a 5,000 bonus
- **GIVEN** 2026 CPF rates
- **WHEN** an employee, opted out of SHG, is paid gross 15,000.00 in March, with OW capped at 8,000 and a 5,000 bonus
- **THEN** EE CPF is 2,600, ER CPF 2,210, SDL 11.25 and net 12,400

#### Scenario: Wage 700 below the S$750 floor
- **GIVEN** an employee is paid gross 700.00 in March, below the S$750 floor
- **WHEN** the expected values are pinned
- **THEN** its CPF-dependent values are re-derived by hand from the CPF Board tables with graduated rates (FR-PAY-02), not taken from the workbook's row (EE 140, ER 119, net 560), which shows the workbook's ⚠ that graduated rates were not applied
- **AND** its SDL stays 2.00 (the minimum for remuneration up to S$800 [verify]; SDL does not depend on the graduated CPF rates), so the March SDL total of S$70.75 and the S$70 payable stand
- **AND** the parity test (QA-MIG-01) lists the row in the migration difference register (FR-MIG-02) instead of expecting the workbook's figures

#### Scenario: Wage 700 with overrides in April
- **GIVEN** an employee paid gross 700.00 in April with overrides
- **WHEN** the pay run is computed
- **THEN** EE CPF is 0, ER CPF 50, SDL 2.00 and net 700, as in the workbook

#### Scenario: Employee aged 55 or under in April
- **GIVEN** 2026 CPF rates
- **WHEN** an employee aged ≤ 55 is paid gross 7,000.00 in April
- **THEN** EE CPF is 1,400, ER CPF 1,190, SDL 11.25, SHG 5.00 and net 5,595
- **AND** the fund and band behind the S$5.00 are confirmed against the CDAC, ECF, MBMF and SINDA tables in the statutory pack before the value is pinned [verify]

#### Scenario: March SDL payable
- **WHEN** the SDL payable for March is computed for the G.2 March employees
- **THEN** the employees' SDL totals S$70.75
- **AND** the March SDL payable is **S$70**

#### Scenario: April SDL payable
- **WHEN** the SDL payable for April is computed for the G.2 April employees
- **THEN** the employees' SDL totals S$13.25
- **AND** the April SDL payable is **S$13**

# people/payroll Specification

## Purpose
Payroll in Nemura: PRD-001 §8.7's Singapore rules implemented, the inputs and calendar of a run, payslips to self-service, the hand-off to the books and final pay. Derived from PRD-002 §9.

## Requirements

### Requirement: NM-PAY-01 The rules are PRD-001's
Nemura's payroll SHALL implement PRD-001 FR-PAY-01…16 (except FR-PAY-12), FR-PAY-17, FR-PAY-18 and Appendix B.3's formulas at the phases of the delivery map (NM-PAY-07), tested by the shared oracle.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Same results
- **WHEN** a pay run is calculated in Nemura
- **THEN** CPF, SDL, SHG and net pay follow PRD-001's rules and equal the oracle's to the cent

### Requirement: NM-PAY-02 Inputs collected for the run
A pay run SHALL collect joiners, leavers, pay changes, unpaid leave and encashment, Part IV overtime, one-off allowances and bonuses, and payroll-paid claims, each linked to its source record.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Linked inputs
- **WHEN** the October run is prepared
- **THEN** each input (for example a joiner's prorated pay or approved overtime) links to the record it came from

#### Scenario: Classed by facts
- **WHEN** a one-off item is added to the run
- **THEN** its CPF class follows PRD-001 FR-PAY-17, not its name

### Requirement: NM-PAY-03 Payroll calendar
Each pay group SHALL have a pay day, an input cut-off and a latest approval date that respect the pack's payment deadlines, and Checks SHALL warn when a run is late.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Late run
- **WHEN** a run is not approved by its latest approval date
- **THEN** Checks shows ⏰ (N.1 #2)

#### Scenario: Statutory payment deadlines
- **GIVEN** a monthly salary period ending Sat 31 Oct
- **WHEN** the pay day is set
- **THEN** salary must be paid within 7 days after the period ends, by Sat 7 Nov, and overtime within 14 days, by Sat 14 Nov

#### Scenario: Final pay
- **WHEN** a person leaves
- **THEN** their final pay follows NM-PAY-08's deadlines

### Requirement: NM-PAY-04 Payslips to self-service
Payslips SHALL be published to each person's My pay within 3 working days of their confirmed payment, with print and download, and leavers SHALL keep access to their payslips (NM-ESS-06).

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Published on pay day
- **WHEN** salaries are paid on Wed 28 Oct
- **THEN** each person's payslip is in My pay by then, or within 3 working days

#### Scenario: Leaver
- **WHEN** a person who has left asks for a payslip
- **THEN** they reach it through their leaver account for the record-keeping period, and an email link only delivers it

#### Scenario: From the payment date
- **GIVEN** a person's payment confirmed paid on Thu 29 Oct
- **WHEN** their payslip deadline is worked out
- **THEN** it counts from 29 Oct (NM-PAY-10)

### Requirement: NM-PAY-05 Hand-off to the books
An approved pay run SHALL post to a linked Fyniq as PRD-001 FR-PPL-04 describes, or produce journal and payment files when standalone, and SHALL record what the books received.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Linked
- **WHEN** a run is approved and Nemura is linked
- **THEN** the summary journal by department and the salary payment lines go to Fyniq, and the run shows the posting reference

#### Scenario: Mismatch
- **WHEN** the books received totals different from the run's
- **THEN** Checks shows ✖ (N.1 #6)

#### Scenario: Standalone manifest
- **GIVEN** Nemura is not linked
- **WHEN** the journal and bank files are exported
- **THEN** each carries a manifest (run, version, totals, digest), and neither is taken as proof of payment

### Requirement: NM-PAY-06 Final pay
A leaver's last salary, leave settlement, notice pay and IR21 clearance hold SHALL be prepared in one place, linked to offboarding.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Leaver with tax clearance
- **GIVEN** an Employment Pass holder leaving on 30 Nov
- **WHEN** final pay is prepared
- **THEN** it includes the leave settlement and holds all monies due until IRAS releases them (PRD-001 FR-PAY-10)

#### Scenario: Deadline by case
- **WHEN** final pay is prepared
- **THEN** its deadline is NM-PAY-08's for the case

### Requirement: NM-PAY-07 Delivery map
Each PRD-001 payroll row SHALL have one owner, Fyniq's part and one phase when Nemura runs payroll, as PRD-002 §9's delivery map sets out; a company on Fyniq without Nemura SHALL meet the same rules through its external system or recorded manual arrangements.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Payslips in P1
- **WHEN** Nemura runs payroll
- **THEN** payslips are delivered from P1, though PRD-001 places them in P2 for a Fyniq-only company

#### Scenario: CPF file in P2
- **WHEN** the CPF submission file is not built yet
- **THEN** a manual CPF EZPay entry is recorded under FR-PAY-15 before a run is approved

#### Scenario: Calculation runs in Nemura
- **WHEN** Nemura runs payroll
- **THEN** pay runs and overrides, CPF, year to date, wage classes, SDL, SHG, the foreign worker levy, deductions and totals are calculated in Nemura from P1
- **AND** Fyniq only posts the run (FR-PPL-04, FR-PPL-09), deductions and totals per Appendix B.3

#### Scenario: Approval in Nemura, verified by Fyniq
- **WHEN** a run is approved in Nemura
- **THEN** Fyniq verifies the approval before it posts (FR-PPL-09…11)

#### Scenario: Year-end forms in P2
- **WHEN** IR8A and AIS are due
- **THEN** Nemura prepares them from P2; the first AIS is due by 1 March after the first full year

#### Scenario: Clearance hold checked at release
- **GIVEN** an IR21 clearance hold kept in Nemura (NM-INT-08)
- **WHEN** Fyniq releases a salary payment batch
- **THEN** it checks the hold first (FR-PPL-13)

#### Scenario: Salary payments
- **WHEN** a run is ready to pay
- **THEN** the payment lines come from Nemura, and approval and release happen in Fyniq (FR-PPL-10, FR-PPL-14)

#### Scenario: Leave balances and the payroll restriction
- **WHEN** Nemura runs payroll
- **THEN** leave balances are Nemura's (FR-PAY-12 is superseded by NM-LV-08)
- **AND** the payroll restriction is kept in Nemura (Appendix B), while Fyniq restricts salary-payment detail (FR-PPL-17)

#### Scenario: Go-live gate
- **WHEN** a run is about to be approved
- **THEN** Nemura records the statutory arrangements first, and Fyniq's release waits for them (FR-PAY-15)

#### Scenario: A company without Nemura
- **GIVEN** a company on Fyniq without Nemura
- **WHEN** it pays employees
- **THEN** it meets the same rules through its external system or recorded manual arrangements

### Requirement: NM-PAY-08 Final pay deadlines by case
Final pay SHALL be due on the last day after resignation with notice, within 7 days after resignation without notice, and on the last day or within 3 working days after misconduct dismissal or employer termination; only a tax-clearance hold SHALL hold it, and for an employer with at least 10 employees the retrenchment notification to MOM SHALL be tracked.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Resignation with notice and a tax-clearance hold
- **GIVEN** Chen Yu resigned with notice, his last day is Mon 30 Nov, and IR21 applies to him
- **WHEN** his final pay is scheduled
- **THEN** it is due on Mon 30 Nov, and held under his tax-clearance hold until IRAS releases it

#### Scenario: Resignation without notice
- **GIVEN** an employee resigns without notice, last day Fri 6 Nov
- **WHEN** final pay is scheduled
- **THEN** it is due by Fri 13 Nov

#### Scenario: Equipment not returned
- **WHEN** a leaver has not returned their laptop
- **THEN** their final pay is not held for it

#### Scenario: Retrenchment notification
- **GIVEN** a company with 12 employees tells 2 of them on Mon 16 Nov that they are retrenched
- **WHEN** the notification to MOM is tracked
- **THEN** it is due by Mon 23 Nov, 5 working days later, and Checks shows it (N.1 #15)

### Requirement: NM-PAY-09 Corrections
A correction SHALL be a new run version naming the version it supersedes with its wage month's full figures; Fyniq SHALL post the difference, only net still unpaid SHALL be paid, an overpayment SHALL become a recovery, and a closed month SHALL keep its statutory wage month.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Correcting a run
- **GIVEN** the October run, version 1, was posted with S$10,000 of wages
- **WHEN** version 2 corrects it to S$10,200
- **THEN** Fyniq posts S$200 more, and only the net pay still unpaid is paid

#### Scenario: Overpayment
- **WHEN** a correction reduces pay already paid
- **THEN** the excess becomes a recovery under PRD-001 FR-PAY-16, never a negative payment

#### Scenario: Closed month
- **GIVEN** September's books are closed
- **WHEN** a September correction is approved in October
- **THEN** Nemura keeps September as the wage month for CPF and IR8A, and the books post it in October

### Requirement: NM-PAY-10 Run and payment states
A run SHALL move from draft to closed and each person's payment from scheduled to paid, rejected, returned or unknown, from Fyniq's status events or recorded evidence; a posted journal or a file SHALL never mean paid, and an unknown outcome SHALL block new payment.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Mixed batch
- **GIVEN** 6 payment lines
- **WHEN** 4 are paid, 1 is rejected and 1 has no outcome
- **THEN** the run is paid in part, and each person's My pay shows their own state
- **AND** Checks shows ✖ for the two (N.1 #14)

#### Scenario: Unknown blocks paying again
- **WHEN** someone tries to pay the line with no outcome again
- **THEN** it is refused until the outcome is resolved

#### Scenario: Standalone outcome
- **GIVEN** Nemura is not linked
- **WHEN** a person records a payment outcome
- **THEN** it needs evidence and is audited

### Requirement: NM-PAY-11 What changed since last month
The approval screen SHALL show the run against the previous month's in gross pay, net pay and employer cost, split by cause, each change linked to its input, and SHALL list any change without an explanation.

Source: PRD-002 §9 · Priority S · Phase P1

#### Scenario: Bonus explained
- **GIVEN** the October run includes Wei Jie Ong's S$5,000.00 bonus
- **WHEN** Arjun Rao opens the run to approve it
- **THEN** the change from September shows the bonus as a one-off item linked to its input
- **AND** any change without an explanation is listed

# controls/platform-checks Specification

## Purpose
The 52 checks Fyniq adds to the workbook's checks: ledger and audit integrity, controls and access, banking and payments, scheduler, tax and payroll deadlines, statutory packs, founders and equity, evidence, corporate secretarial duties, AI agents, email, integrations, claims, projects and the close. Derived from PRD-001 Appendix C.2.

## Requirements

### Requirement: C.2 #1 Materialised balances equal recomputation
The system SHALL check nightly that the materialised balances equal their recomputation from the journal lines (INV-08); a difference blocks the close.

Source: PRD-001 §C.2

#### Scenario: A materialised balance differs from its recomputation
- **WHEN** a materialised balance differs from its recomputation from the lines
- **THEN** the check fails and blocks the close

#### Scenario: Balances agree
- **WHEN** every materialised balance equals its recomputation from the lines
- **THEN** the check passes

### Requirement: C.2 #2 Subledgers equal control accounts
The system SHALL check nightly that each subledger equals its control account (INV-07); a difference blocks the close.

Source: PRD-001 §C.2

#### Scenario: A subledger differs from its control account
- **WHEN** the AR, AP, payroll liabilities, fixed assets, GST, prepayments, deferred revenue or expense claims subledger differs from its control account
- **THEN** the check fails and blocks the close

#### Scenario: Subledgers agree
- **WHEN** every subledger equals its control account
- **THEN** the check passes

### Requirement: C.2 #3 Audit hash chain intact and anchored
The system SHALL check nightly that the audit hash chain is intact and anchored (INV-12); a failure blocks the close.

Source: PRD-001 §C.2

#### Scenario: The hash chain is broken
- **WHEN** the audit hash chain is not intact
- **THEN** the check fails and blocks the close

#### Scenario: The hash chain is not anchored
- **WHEN** the audit hash chain is not anchored
- **THEN** the check fails and blocks the close

#### Scenario: Chain intact and anchored
- **WHEN** the audit hash chain is intact and anchored
- **THEN** the check passes

### Requirement: C.2 #4 Self-approved items unreviewed after 14 days
The system SHALL flag for review, on every relevant change and nightly, self-approved items not reviewed within 14 days (FM-3).

Source: PRD-001 §C.2

#### Scenario: A self-approved item is unreviewed after 14 days
- **WHEN** a self-approved item has not been reviewed within 14 days
- **THEN** the check flags it for review

#### Scenario: Reviewed within 14 days
- **WHEN** a self-approved item is reviewed within 14 days
- **THEN** the check does not flag it

### Requirement: C.2 #5 SoD conflicts outside the team mode
The system SHALL check, on every relevant change and nightly, for users with SoD conflicts not covered by the team mode; a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: An SoD conflict the team mode does not cover
- **WHEN** a user has an SoD conflict that the team mode does not cover
- **THEN** the check fails and blocks the close

#### Scenario: An SoD conflict the team mode covers
- **WHEN** a user's SoD conflict is covered by the team mode
- **THEN** the check does not report that user

### Requirement: C.2 #6 Expired external users still active
The system SHALL check, on every relevant change and nightly, for external users past their expiry date who are still active; a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: An external user is active past their expiry date
- **WHEN** an external user is still active after their expiry date
- **THEN** the check fails and blocks the close

#### Scenario: No expired external user is active
- **WHEN** no external user past their expiry date is active
- **THEN** the check passes

### Requirement: C.2 #7 Access review overdue
The system SHALL flag for review, on every relevant change and nightly, an access review that is overdue.

Source: PRD-001 §C.2

#### Scenario: The access review is overdue
- **WHEN** the access review is overdue
- **THEN** the check flags it for review

#### Scenario: The access review is up to date
- **WHEN** the access review is not overdue
- **THEN** the check passes

### Requirement: C.2 #8 Bank feed stale over 3 days
The system SHALL flag for review, on every relevant change and nightly, a bank feed not updated for more than 3 days.

Source: PRD-001 §C.2

#### Scenario: A feed is not updated for more than 3 days
- **WHEN** a bank feed has not been updated for more than 3 days
- **THEN** the check flags it for review

#### Scenario: A feed updated within 3 days
- **WHEN** a bank feed was updated within the last 3 days
- **THEN** the check does not flag it

### Requirement: C.2 #9 Unreconciled lines older than 30 days
The system SHALL flag for review, on every relevant change and nightly, unreconciled bank lines older than 30 days.

Source: PRD-001 §C.2

#### Scenario: An unreconciled line is older than 30 days
- **WHEN** a bank line older than 30 days is unreconciled
- **THEN** the check flags it for review

#### Scenario: A recent unreconciled line
- **WHEN** an unreconciled bank line is 30 days old or less
- **THEN** the check does not flag it

### Requirement: C.2 #10 Possible duplicate bills
The system SHALL flag for review, on every relevant change and nightly, possible duplicate bills: the same vendor and amount within a date window.

Source: PRD-001 §C.2

#### Scenario: Same vendor, amount and date window
- **WHEN** two bills have the same vendor and the same amount and are dated within the date window
- **THEN** the check flags them for review as possible duplicates

#### Scenario: Different vendor, amount or dates
- **WHEN** two bills differ in vendor or amount, or are dated outside the date window
- **THEN** the check does not flag them

### Requirement: C.2 #11 Changed vendor bank details, payments pending
The system SHALL flag for review, on every relevant change and nightly, a vendor whose bank details changed in the last 30 days and who has payments pending.

Source: PRD-001 §C.2

#### Scenario: Changed bank details with payments pending
- **GIVEN** a vendor's bank details changed in the last 30 days
- **WHEN** payments to that vendor are pending
- **THEN** the check flags the vendor for review

#### Scenario: Bank details not changed in the last 30 days
- **GIVEN** a vendor's bank details have not changed in the last 30 days
- **WHEN** payments to that vendor are pending
- **THEN** the check does not flag the vendor

### Requirement: C.2 #12 Scheduled job failed or late
The system SHALL check, on every relevant change and nightly, for scheduled jobs (recurring, depreciation, revaluation, reminders) that failed or are late; a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: A scheduled job failed
- **WHEN** a scheduled job failed
- **THEN** the check fails and blocks the close

#### Scenario: A scheduled job is late
- **WHEN** a scheduled job is late
- **THEN** the check fails and blocks the close

#### Scenario: A dead outbox row
- **WHEN** an outbox row is dead after the retry limit
- **THEN** it appears on this check with a manual replay (§12.11)

#### Scenario: Jobs ran on time
- **WHEN** every scheduled job has run on time
- **THEN** the check passes

### Requirement: C.2 #13 GST return unfiled or due soon
The system SHALL flag as due, on every relevant change and nightly, a GST return whose period ended and that is not filed, and a GST filing due within 7 days.

Source: PRD-001 §C.2

#### Scenario: Return period ended and not filed
- **WHEN** a GST return period has ended and the return is not filed
- **THEN** the check flags the return as due

#### Scenario: Filing due within 7 days
- **WHEN** a GST filing is due within 7 days
- **THEN** the check flags it as due

#### Scenario: Return filed
- **WHEN** the GST return for an ended period is filed
- **THEN** the check does not flag it

### Requirement: C.2 #14 CPF payment due, not paid
The system SHALL flag as due, on every relevant change and nightly, a CPF payment due by the 14th that is not yet paid.

Source: PRD-001 §C.2

#### Scenario: CPF payment not yet paid
- **WHEN** a CPF payment due by the 14th has not yet been paid
- **THEN** the check flags it as due

#### Scenario: CPF payment paid
- **WHEN** the CPF payment has been paid
- **THEN** the check does not flag it

### Requirement: C.2 #15 Statutory deadline within 14 days
The system SHALL flag as due, on every relevant change and nightly, a statutory deadline (ECI, Form C-S, AIS, annual return) within 14 days.

Source: PRD-001 §C.2

#### Scenario: A statutory deadline within 14 days
- **WHEN** an ECI, Form C-S, AIS or annual return deadline is within 14 days
- **THEN** the check flags it as due

#### Scenario: A deadline more than 14 days away
- **WHEN** a statutory deadline is more than 14 days away
- **THEN** the check does not flag it

### Requirement: C.2 #16 Statutory parameter pack out of date
The system SHALL flag for review, on every relevant change and nightly, a statutory parameter pack past its review date or older than the latest published version; both triggers apply.

Source: PRD-001 §C.2

#### Scenario: A pack older than the latest published version
- **WHEN** a statutory parameter pack in use is older than the latest published version
- **THEN** the check flags it for review

#### Scenario: A pack past its review date
- **WHEN** a statutory parameter pack is past its review date
- **THEN** the check flags it for review (ENG-30)

#### Scenario: Packs are current
- **WHEN** every pack in use is the latest published version and within its review date
- **THEN** the check passes

### Requirement: C.2 #17 Foreign-currency items not revalued
The system SHALL flag for review, on every relevant change and nightly, foreign-currency items not revalued at the last month end.

Source: PRD-001 §C.2

#### Scenario: An item not revalued at the last month end
- **WHEN** a foreign-currency item was not revalued at the last month end
- **THEN** the check flags it for review

#### Scenario: Every item revalued
- **WHEN** every foreign-currency item was revalued at the last month end
- **THEN** the check passes

### Requirement: C.2 #18 Month unclosed after 15 days
The system SHALL flag for review, on every relevant change and nightly, a month that ended more than 15 days ago and is not closed.

Source: PRD-001 §C.2

#### Scenario: A month ended more than 15 days ago is not closed
- **WHEN** a month ended more than 15 days ago and is not closed
- **THEN** the check flags it for review

#### Scenario: A month ended 15 days ago or less
- **WHEN** a month that is not closed ended 15 days ago or less
- **THEN** the check does not flag it

### Requirement: C.2 #19 AI proposals pending over 48 hours
The system SHALL flag for review, on every relevant change and nightly, AI proposals pending approval for more than 48 hours.

Source: PRD-001 §C.2

#### Scenario: A proposal pending for more than 48 hours
- **WHEN** an AI proposal has been pending approval for more than 48 hours
- **THEN** the check flags it for review

#### Scenario: A proposal pending for 48 hours or less
- **WHEN** an AI proposal has been pending approval for 48 hours or less
- **THEN** the check does not flag it

### Requirement: C.2 #20 Posted bills or claims without a document
The system SHALL flag for review, on every relevant change and nightly, posted bills or claims with no receipt or invoice attached; entry is already blocked by FR-DOC-06, so this catches imported and migrated ones.

Source: PRD-001 §C.2

#### Scenario: A posted bill or claim has no document
- **WHEN** a posted bill or claim, for example an imported or migrated one, has no receipt or invoice attached
- **THEN** the check flags it for review, whatever its amount

#### Scenario: A document is attached
- **WHEN** a posted bill or claim has its receipt or invoice attached
- **THEN** the check does not flag it

### Requirement: C.2 #21 Director's account in debit
The system SHALL flag for review, on every relevant change and nightly, a director's account that is in debit at a month end (FR-EQ-11).

Source: PRD-001 §C.2

#### Scenario: A director's account in debit at a month end
- **WHEN** a director's account is in debit at a month end
- **THEN** the check flags it for review

#### Scenario: No director's account in debit
- **WHEN** no director's account is in debit at a month end
- **THEN** the check passes

### Requirement: C.2 #22 Share-issue lodgement and conversion registration
The system SHALL flag as due, on every relevant change and nightly, a share-issue approval not lodged within 14 days of the resolution, an approved conversion not yet registered by ACRA, or its approval lapsing at the next AGM (FR-EQ-09).

Source: PRD-001 §C.2

#### Scenario: Approval not lodged within 14 days
- **WHEN** a share-issue approval has not been lodged within 14 days of the resolution
- **THEN** the check flags it as due

#### Scenario: Approved conversion not yet registered
- **WHEN** an approved conversion is not yet registered by ACRA
- **THEN** the check flags it as due

#### Scenario: Approval lapsing at the next AGM
- **WHEN** an approved conversion's approval will lapse at the next AGM
- **THEN** the check flags it as due

### Requirement: C.2 #23 Director's closing balance not confirmed
The system SHALL flag for review, on every relevant change and nightly, a director's closing balance not confirmed at the year end (FR-EQ-12).

Source: PRD-001 §C.2

#### Scenario: Closing balance not confirmed
- **WHEN** a director's closing balance at the year end has not been confirmed
- **THEN** the check flags it for review

#### Scenario: Closing balance confirmed
- **WHEN** every director's closing balance at the year end has been confirmed
- **THEN** the check passes

### Requirement: C.2 #24 Director's pay deferred without choice
The system SHALL check, on every relevant change and nightly, for a director's pay deferred without a recorded pay choice (FR-EQ-04); a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: Pay deferred with no recorded pay choice
- **WHEN** a director's pay is deferred without a recorded pay choice
- **THEN** the check fails and blocks the close

#### Scenario: Pay deferred under a recorded pay choice
- **GIVEN** a director's recorded pay choice is salary deferred
- **WHEN** the director's pay is deferred
- **THEN** the check does not report it

### Requirement: C.2 #25 Share capital differs from the register
The system SHALL check, on every relevant change and nightly, that the share-capital balance equals the register of share issues (INV-13); a difference blocks the close.

Source: PRD-001 §C.2

#### Scenario: Share capital differs from the register
- **WHEN** the share-capital balance differs from the register of share issues
- **THEN** the check fails and blocks the close

#### Scenario: Share capital agrees
- **WHEN** the share-capital balance equals the register of share issues
- **THEN** the check passes

### Requirement: C.2 #26 Equity register differs from ACRA
The system SHALL flag for review, on every relevant change and nightly, an equity register that differs from the latest uploaded ACRA register-of-members extract (FR-EQ-26).

Source: PRD-001 §C.2

#### Scenario: Register differs from the extract
- **WHEN** the equity register differs from the latest uploaded ACRA register-of-members extract
- **THEN** the check flags it for review

#### Scenario: Register agrees with the extract
- **WHEN** the equity register agrees with the latest uploaded extract
- **THEN** the check passes

### Requirement: C.2 #27 Controller threshold crossed, register not updated
The system SHALL flag as due, on every relevant change and nightly, a holder who crossed the controller threshold while the register of controllers is not yet updated (FR-EQ-25).

Source: PRD-001 §C.2

#### Scenario: Register of controllers not yet updated
- **WHEN** a holder has crossed the controller threshold and the register of controllers is not yet updated
- **THEN** the check flags it as due

#### Scenario: Register of controllers updated
- **WHEN** the register of controllers has been updated for the holder
- **THEN** the check does not flag it

### Requirement: C.2 #28 Exempt-private-company status changed
The system SHALL flag for review, on every relevant change and nightly, a change in exempt-private-company status, so that the directors' debit balances are re-checked (FR-EQ-11, FR-EQ-25).

Source: PRD-001 §C.2

#### Scenario: Status changed
- **WHEN** the company's exempt-private-company status changes
- **THEN** the check flags for review that the directors' debit balances must be re-checked

### Requirement: C.2 #29 SAFE or note maturity and conversion
The system SHALL flag for review, on every relevant change and nightly, a SAFE or note past maturity, or a priced round recorded without its conversions (FR-EQ-18).

Source: PRD-001 §C.2

#### Scenario: SAFE or note past maturity
- **WHEN** a SAFE or a note is past its maturity
- **THEN** the check flags it for review

#### Scenario: Priced round without its conversions
- **WHEN** a priced round is recorded without its conversions
- **THEN** the check flags it for review

### Requirement: C.2 #30 Completed activities missing recommended evidence
The system SHALL flag for review, on every relevant change and nightly, completed activities missing recommended evidence (FR-DOC-06).

Source: PRD-001 §C.2

#### Scenario: Recommended evidence missing
- **WHEN** a completed activity is missing evidence its evidence rule recommends
- **THEN** the check flags it for review

#### Scenario: Recommended evidence present
- **WHEN** a completed activity has the evidence its evidence rule recommends
- **THEN** the check does not flag it

### Requirement: C.2 #31 Evidence hash re-verification failed
The system SHALL re-verify evidence hashes nightly (FR-DOC-07); a failed re-verification blocks the close.

Source: PRD-001 §C.2

#### Scenario: An evidence hash fails re-verification
- **WHEN** the nightly re-verification of an evidence item's hash fails
- **THEN** the check fails and blocks the close

#### Scenario: Every evidence hash re-verifies
- **WHEN** every evidence hash re-verifies
- **THEN** the check passes

### Requirement: C.2 #32 No company secretary after 6 months
The system SHALL check, on every relevant change and nightly, that a company secretary is recorded 6 months after incorporation (FR-CS-06); a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: No secretary after 6 months
- **WHEN** 6 months have passed since incorporation and no company secretary is recorded
- **THEN** the check fails and blocks the close

#### Scenario: Secretary recorded
- **WHEN** a company secretary is recorded
- **THEN** the check passes

### Requirement: C.2 #33 Financial statements unapproved near their deadline
The system SHALL flag as due, on every relevant change and nightly, financial statements not approved when their deadline (AGM, or sending to members) is within 30 days (FR-CS-03).

Source: PRD-001 §C.2

#### Scenario: Deadline within 30 days and statements not approved
- **WHEN** the financial statements are not approved and their deadline (the AGM, or sending them to members) is within 30 days
- **THEN** the check flags them as due

#### Scenario: Statements approved
- **WHEN** the financial statements are approved
- **THEN** the check does not flag them

### Requirement: C.2 #34 Corporate profile differs from ACRA
The system SHALL flag for review, on every relevant change and nightly, a corporate profile that differs from the latest uploaded ACRA business-profile extract (FR-CS-01).

Source: PRD-001 §C.2

#### Scenario: Profile differs from the extract
- **WHEN** the corporate profile differs from the latest uploaded ACRA business-profile extract
- **THEN** the check flags it for review

#### Scenario: Profile agrees with the extract
- **WHEN** the corporate profile agrees with the latest uploaded extract
- **THEN** the check passes

### Requirement: C.2 #35 Corporate change without lodgement evidence
The system SHALL flag as due, on every relevant change and nightly, an officer, registered-office or controller change without its lodgement evidence by the deadline (FR-CS-01).

Source: PRD-001 §C.2

#### Scenario: No lodgement evidence by the deadline
- **WHEN** an officer, registered-office or controller change has no lodgement evidence by its deadline
- **THEN** the check flags it as due

#### Scenario: Lodgement evidence recorded
- **WHEN** the change has its lodgement evidence by the deadline
- **THEN** the check does not flag it

### Requirement: C.2 #36 Agent run failed, skipped or late
The system SHALL flag for review, on every relevant change and nightly, a scheduled agent run that failed, was skipped or is late (AI-67).

Source: PRD-001 §C.2

#### Scenario: Agent run failed
- **WHEN** a scheduled agent run failed
- **THEN** the check flags it for review

#### Scenario: Agent run skipped
- **WHEN** a scheduled agent run was skipped
- **THEN** the check flags it for review

#### Scenario: Agent run late
- **WHEN** a scheduled agent run is late
- **THEN** the check flags it for review

### Requirement: C.2 #37 Sending domain alignment, bounces and complaints
The system SHALL flag for review, on every relevant change and nightly, a sending domain that fails SPF, DKIM or DMARC alignment, or bounces or complaints above the threshold (FR-INT-21).

Source: PRD-001 §C.2

#### Scenario: Domain fails alignment
- **WHEN** the sending domain fails SPF, DKIM or DMARC alignment
- **THEN** the check flags it for review

#### Scenario: Bounces or complaints exceed the threshold
- **WHEN** bounces or complaints exceed the threshold
- **THEN** the check flags it for review

### Requirement: C.2 #38 Integration connection failing or credentials expiring
The system SHALL flag for review, on every relevant change and nightly, an integration connection failing its health check, or whose credentials expire within 14 days (FR-INT-20).

Source: PRD-001 §C.2

#### Scenario: Health check failing
- **WHEN** an integration connection is failing its health check
- **THEN** the check flags it for review

#### Scenario: Credentials expire within 14 days
- **WHEN** an integration connection's credentials expire within 14 days
- **THEN** the check flags it for review

### Requirement: C.2 #39 Cash advance unsettled by claim deadline
The system SHALL flag for review, on every relevant change and nightly, a cash advance not settled by the claim deadline (FR-AP-17).

Source: PRD-001 §C.2

#### Scenario: Advance unsettled after the claim deadline
- **WHEN** a cash advance is not settled by the claim deadline
- **THEN** the check flags it for review

#### Scenario: Advance settled
- **WHEN** a cash advance is settled by the claim deadline
- **THEN** the check does not flag it

### Requirement: C.2 #40 Approved time not invoiced
The system SHALL flag for review, on every relevant change and nightly, approved time not invoiced 30 days after the period end on a time-and-materials project (FR-PRJ-10).

Source: PRD-001 §C.2

#### Scenario: Approved time unbilled after 30 days
- **WHEN** approved time on a time-and-materials project is not invoiced 30 days after the period end
- **THEN** the check flags it for review

#### Scenario: Approved time invoiced
- **WHEN** the approved time has been invoiced
- **THEN** the check does not flag it

### Requirement: C.2 #41 Missing or unapproved timesheets
The system SHALL flag for review, on every relevant change and nightly, missing or unapproved timesheets for a closed week (FR-PRJ-05/07).

Source: PRD-001 §C.2

#### Scenario: Timesheet missing for a closed week
- **WHEN** a timesheet for a closed week is missing
- **THEN** the check flags it for review

#### Scenario: Timesheet unapproved for a closed week
- **WHEN** a timesheet for a closed week is not approved
- **THEN** the check flags it for review

### Requirement: C.2 #42 Provider clearing not netting to zero
The system SHALL check, on every relevant change and nightly, that a payment-provider clearing account nets to zero for each settled payout (FR-PSP-06); a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: Clearing does not net to zero
- **WHEN** a payment-provider clearing account does not net to zero for a settled payout
- **THEN** the check fails and blocks the close

#### Scenario: Clearing nets to zero
- **WHEN** the clearing account nets to zero for every settled payout
- **THEN** the check passes

### Requirement: C.2 #43 Bank sync or provider sweep failing
The system SHALL flag for review, on every relevant change and nightly, a bank data sync that failed or is older than its schedule allows (FR-BNK-15), and a payment-provider completeness sweep that failed or found a gap it could not fill (FR-PSP-01).

Source: PRD-001 §C.2

#### Scenario: Bank data sync failed
- **WHEN** a bank data sync failed
- **THEN** the check flags it for review

#### Scenario: Bank data older than the schedule allows
- **WHEN** a bank account's synced data is older than its schedule allows
- **THEN** the check flags it for review

#### Scenario: Completeness sweep failed
- **WHEN** a payment-provider completeness sweep failed
- **THEN** the check flags it for review

#### Scenario: Gap the sweep could not fill
- **WHEN** a payment-provider completeness sweep found a gap it could not fill
- **THEN** the check flags it for review

### Requirement: C.2 #44 Payment during an IR21 clearance hold
The system SHALL check, on every relevant change and nightly, for a payment to an employee under an IR21 clearance hold released or scheduled before IRAS's notification (FR-PAY-10); a finding blocks the close.

Source: PRD-001 §C.2

#### Scenario: Payment released before IRAS's notification
- **GIVEN** an employee under an IR21 clearance hold
- **WHEN** a payment to that employee is released before IRAS's notification
- **THEN** the check fails and blocks the close

#### Scenario: Payment scheduled before IRAS's notification
- **GIVEN** an employee under an IR21 clearance hold
- **WHEN** a payment to that employee is scheduled before IRAS's notification
- **THEN** the check fails and blocks the close

#### Scenario: Payment after IRAS's notification
- **WHEN** a payment to the employee is released after IRAS's notification
- **THEN** the check does not report it

### Requirement: C.2 #45 Salary payment without a payslip
The system SHALL flag as due, on every relevant change and nightly, a salary payment with no payslip issued within 3 working days (FR-PAY-08, FR-PAY-15).

Source: PRD-001 §C.2

#### Scenario: No payslip within 3 working days
- **WHEN** no payslip has been issued within 3 working days of a salary payment
- **THEN** the check flags it as due

#### Scenario: Without Nemura
- **GIVEN** a company that pays staff without Nemura
- **WHEN** salary is paid
- **THEN** the check confirms the FR-PAY-15 arrangement is recorded, since Fyniq issues no payslips

#### Scenario: Payslip issued in time
- **WHEN** the payslip is issued within 3 working days of the salary payment
- **THEN** the check does not flag it

### Requirement: C.2 #46 GST control roll-forward does not reconcile
The system SHALL check, on every relevant change and nightly, that the GST control roll-forward reconciles for each return period (INV-07); a failure blocks the close.

Source: PRD-001 §C.2

#### Scenario: Roll-forward does not reconcile
- **WHEN** the GST control roll-forward does not reconcile for a return period
- **THEN** the check fails and blocks the close

#### Scenario: Roll-forward reconciles
- **WHEN** the GST control roll-forward reconciles for every return period
- **THEN** the check passes

### Requirement: C.2 #47 GST adjustment without a tax-agent decision
The system SHALL flag as due, on every relevant change and nightly, a GST adjustment for a filed period with no tax-agent decision (next F5 or F7) 14 days before the next return is due (FR-TAX-12).

Source: PRD-001 §C.2

#### Scenario: No decision 14 days before the next return is due
- **GIVEN** a GST adjustment for a filed period
- **WHEN** the next return is due in 14 days and the tax agent has not decided between the next F5 and an F7
- **THEN** the check flags it as due

#### Scenario: Tax agent has decided
- **WHEN** the tax agent has decided next F5 or F7 for the adjustment
- **THEN** the check does not flag it

### Requirement: C.2 #48 Unpaid bill with claimed input tax
The system SHALL flag for review, on every relevant change and nightly, a bill with input tax claimed that is unpaid 12 months after its due date, as the input tax must then be repaid (IRAS).

Source: PRD-001 §C.2

#### Scenario: G4-037 Bill with input tax unpaid 12 months after its due date
- **GIVEN** a bill with input tax claimed
- **WHEN** it is still unpaid 12 months after its due date
- **THEN** Checks flags it for review with the repayment rule

#### Scenario: Bill paid within 12 months
- **WHEN** a bill with input tax claimed is paid before 12 months after its due date
- **THEN** the check does not flag it

### Requirement: C.2 #49 Settlement residuals and write-off approvals
The system SHALL flag for review, on every relevant change and nightly, settlement residuals written to 7030 this month above the monthly cap, or a write-off approved by the person who recorded the receipt (FR-BNK-08, SOD-9).

Source: PRD-001 §C.2

#### Scenario: Residuals exceed the monthly cap
- **WHEN** the settlement residuals written to 7030 this month exceed the monthly cap
- **THEN** the check flags them for review

#### Scenario: Write-off approved by the person who recorded the receipt
- **WHEN** a write-off was approved by the person who recorded the receipt
- **THEN** the check flags it for review

### Requirement: C.2 #50 Provider balance or fee invoice mismatch
The system SHALL flag for review, on every relevant change and nightly, a provider clearing balance that differs from the provider's reported balance not yet paid out, or a provider fee tax invoice that does not agree to the month's fees (FR-PSP-05/06).

Source: PRD-001 §C.2

#### Scenario: Clearing balance differs from the provider's balance
- **WHEN** a provider clearing balance, per provider and currency, differs from the provider's reported balance not yet paid out
- **THEN** the check flags it for review

#### Scenario: Fee tax invoice does not agree
- **WHEN** a provider fee tax invoice does not agree to the month's fees
- **THEN** the check flags it for review

### Requirement: C.2 #51 Possible duplicate bank lines not resolved
The system SHALL flag for review, on every relevant change and nightly, bank lines marked possible duplicate and not yet resolved (FR-BNK-04, FR-BNK-16).

Source: PRD-001 §C.2

#### Scenario: Possible duplicate not resolved
- **WHEN** a bank line is marked possible duplicate and is not yet resolved
- **THEN** the check flags it for review

#### Scenario: Possible duplicate resolved
- **WHEN** a bank line marked possible duplicate has been resolved
- **THEN** the check does not flag it

### Requirement: C.2 #52 Expected cost without a bill
From P2, the system SHALL flag for review an expected cost without a bill: a supplier billed in each of the last three months, or a PO or supplier contract (FR-AP-21) whose service period has passed, with no bill or accrual for the month being closed.

Source: PRD-001 §C.2

#### Scenario: Regular supplier with no bill
- **WHEN** a supplier billed in each of the last three months has no bill or accrual for the month being closed
- **THEN** the check flags it for review

#### Scenario: PO or contract with no bill
- **WHEN** a PO or supplier contract (FR-AP-21) whose service period has passed has no bill or accrual for the month being closed
- **THEN** the check flags it for review

#### Scenario: Bill or accrual recorded
- **WHEN** a bill or an accrual for the month being closed is recorded for the expected cost
- **THEN** the check does not flag it
### Requirement: C.2 #53 Unreviewed break-glass
The system SHALL report as blocking, on every relevant change and nightly, a break-glass use not reviewed within FM-8's time limit.

Source: PRD-001 §C.2

#### Scenario: Break-glass not reviewed in time
- **WHEN** a break-glass use has not been reviewed within FM-8's time limit
- **THEN** the check reports ✖
- **AND** the monthly close attestation is blocked (FM-4)

#### Scenario: Break-glass reviewed
- **WHEN** every break-glass use has been reviewed in time
- **THEN** the check passes

### Requirement: C.2 #54 Company secretary missing or a sole director as secretary
The system SHALL report as blocking, on every relevant change and nightly, no company secretary recorded 6 months after incorporation, or a sole director recorded as the secretary (FR-CS-06).

Source: PRD-001 §C.2

#### Scenario: No secretary after 6 months
- **WHEN** no company secretary is recorded 6 months after incorporation
- **THEN** the check reports ✖

#### Scenario: Sole director as secretary
- **WHEN** the only director is recorded as the company secretary
- **THEN** the check reports ✖

#### Scenario: Secretary in place
- **WHEN** a company secretary other than a sole director is recorded
- **THEN** the check passes

### Requirement: C.2 #56 Automatic reversals not posted
The system SHALL warn in the close checks when a journal reverses by itself on or before the period's end, so that closing the period stops its reversal, or when a journal dated in the period waits to reverse in a period that is not open (FR-GL-02).

Source: PRD-001 §C.2

#### Scenario: A reversal due in the period
- **GIVEN** a posted journal that reverses by itself on a day on or before the period's end, and has not reversed yet
- **WHEN** the period's close checks run
- **THEN** the check warns (⚠) that closing the period stops its reversal

#### Scenario: A reversal waiting from the period
- **GIVEN** a posted journal dated in the period whose reversal date no open period covers
- **WHEN** the period's close checks run
- **THEN** the check warns (⚠) that it waits for that period to be opened

#### Scenario: Nothing waits
- **WHEN** no automatic reversal is due by the period's end and none from the period waits
- **THEN** the check passes

### Requirement: C.2 #57 Variances without an explanation
The system SHALL refuse the close (✖) while an income or expense account's total for the period differs from its total for the previous period by more than both the variance amount and the variance percentage of the organisation's settings (the amount alone when the previous total is zero), and has no explanation of the two totals as they are (FR-GL-14).

Source: PRD-001 §C.2

#### Scenario: A variance without an explanation
- **GIVEN** an income or expense account whose change against the previous period is more than both thresholds
- **WHEN** the period's close checks run and no explanation of its two totals as they are exists
- **THEN** the check refuses the close (✖)

#### Scenario: A stale explanation
- **GIVEN** an explanation of a variance whose totals have changed since it was written
- **WHEN** the period's close checks run
- **THEN** the check refuses the close (✖) until a new explanation is written

#### Scenario: Every variance explained, or nothing to compare
- **WHEN** every variance above both thresholds has an explanation of its totals as they are, or the period has no previous period
- **THEN** the check passes

### Requirement: C.2 #55 Model route needs attention
The system SHALL flag for review an agent's route that fell back more often than its daily threshold, runs on a moving model alias not re-qualified this week, is within 20% of its budget or quota, or names a model that is retiring within 30 days, retired or no longer listed, and a connection whose model list has not been read for 3 days (AI-76…AI-78, AI-80, AI-81).

Source: PRD-001 §C.2

#### Scenario: Frequent fallbacks
- **WHEN** a route falls back more often than its daily threshold
- **THEN** the check flags it for review

#### Scenario: Moving alias not re-qualified
- **WHEN** a route uses a moving model alias that has not been re-qualified this week
- **THEN** the check flags it for review

#### Scenario: Close to budget or quota
- **WHEN** a route is within 20% of its budget or quota
- **THEN** the check flags it for review

#### Scenario: Model retiring soon
- **WHEN** a model on a route is deprecated and its retirement date is within 30 days
- **THEN** the check flags it for review

#### Scenario: Model retired or no longer listed
- **WHEN** a model on a route is retired or no longer listed by its provider
- **THEN** the check flags it for review, and the route runs on its next step

#### Scenario: Stale model list
- **WHEN** a connection with a stored credential has had no successful model-list read for 3 days
- **THEN** the check flags it for review

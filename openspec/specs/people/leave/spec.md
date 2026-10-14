# people/leave Specification

## Purpose
Leave and absence in Nemura: leave types, balances, applying and approving, the team calendar, medical certificates, government-paid leave, leave into pay and the books, and flexible-work requests. Derived from PRD-002 §8.

## Requirements

### Requirement: NM-LV-01 Leave types
Leave types SHALL come from the pack and company policy, each with eligibility, unit, pay, payer, document need and approval rule.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Statutory and company types
- **WHEN** the leave types are listed
- **THEN** statutory types from the pack and company types from policy both appear, each with its rules

#### Scenario: Eligibility facts
- **WHEN** eligibility for childcare leave is worked out
- **THEN** it reads the person's dependants (NM-PPL-07) and legal classification (NM-PPL-08)

### Requirement: NM-LV-02 Entitlement and balance
Each person's balance per type SHALL show entitled, taken, booked and left, from accrual or grant, proration, statutory minimums, carry-forward and encashment rules.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Balance shown
- **WHEN** an employee opens their annual leave
- **THEN** they see entitled, taken, booked and left, and can see how it was calculated

#### Scenario: From the ledger
- **WHEN** a balance is shown
- **THEN** it is the sum of the person's leave ledger entries (NM-LV-13)

### Requirement: NM-LV-03 Apply in under a minute
The leave form SHALL show the balance after the request, count the request on its type's basis (NM-LV-11), show who else is away, warn on a clash and refuse a request beyond the balance unless the type allows it.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Working days exclude holidays
- **WHEN** an employee whose rest day is Sunday asks for annual leave from Tue 10 Nov to Fri 13 Nov 2026
- **THEN** 4 working days are counted, and Mon 9 Nov (Deepavali, Sun 8 Nov, observed for people whose rest day is Sunday) is not part of the request

#### Scenario: Beyond the balance
- **WHEN** the request is more than the balance and the type does not allow a negative balance
- **THEN** it is refused with the balance

#### Scenario: Continuous maternity block
- **WHEN** a continuous maternity block is requested
- **THEN** every calendar day counts, holidays and rest days included (NM-LV-11)

### Requirement: NM-LV-04 Approval
The line manager SHALL approve leave, HR SHALL be notified or approve when the type needs it, and a refusal SHALL need a reason.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Manager approves
- **WHEN** the line manager approves a request
- **THEN** the employee and HR are told, and the decision is audited

#### Scenario: Refusal
- **WHEN** a request is refused
- **THEN** the approver must give a reason

### Requirement: NM-LV-05 Team calendar
The team calendar SHALL show leave, worksite holidays and working patterns per month, and SHALL show "Away" instead of the leave type to people without the right.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Away, not the reason
- **WHEN** a colleague without HR rights views the calendar
- **THEN** another person's sick leave shows as "Away"

#### Scenario: Holiday on a rest day
- **GIVEN** an employee whose rest day is Monday
- **WHEN** a public holiday falls on a Monday
- **THEN** the calendar shows their next working day as the holiday

### Requirement: NM-LV-06 Medical certificates
Sick leave SHALL record its certificate and issuer, HR SHALL verify it, and medical details SHALL be restricted.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Verify
- **WHEN** an employee records sick leave with a certificate photo
- **THEN** HR sees it to verify, and other roles do not

#### Scenario: Notified, not applied for
- **WHEN** an employee calls in sick
- **THEN** NM-LV-12's notification rules apply

### Requirement: NM-LV-07 Government-paid leave
Nemura SHALL record the government-paid portion of maternity, paternity, shared parental, childcare, extended childcare and adoption leave and track the employer's reimbursement claim, filed by a person on the Government-Paid Leave portal.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Claim tracked
- **WHEN** paternity leave is taken
- **THEN** the government-paid amount and the claim's date, status and evidence are tracked

#### Scenario: Before the deadline
- **WHEN** a claim is not filed 30 days before its deadline
- **THEN** Checks shows ⏰ (N.1 #10)

#### Scenario: Per scheme
- **WHEN** a claim is tracked
- **THEN** its cap and deadline are its scheme's (NM-LV-14)

### Requirement: NM-LV-08 Leave into pay
Unpaid leave and leave encashment SHALL become pay-run inputs; this supersedes PRD-001 FR-PAY-12.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Unpaid leave
- **WHEN** an employee takes 2 days of unpaid leave in a month
- **THEN** the pay run includes the deduction from Nemura's leave record

#### Scenario: Gross formula
- **WHEN** unpaid leave is deducted
- **THEN** gross pay follows PRD-001 Appendix B.3

### Requirement: NM-LV-09 Leave into the books
Leave taken SHALL update available hours in Fyniq, and an optional month-end leave liability accrual SHALL go to Fyniq as a journal for approval.

Source: PRD-002 §8 · Priority S · Phase P1 / P2 (accrual)

#### Scenario: Available hours
- **WHEN** leave is approved and taken
- **THEN** Fyniq's available hours for that person drop by those hours (PRD-001 FR-PRJ-12)

#### Scenario: The policy decides recognition
- **WHEN** the automatic accrual is off
- **THEN** whether untaken leave is recognised still follows the accounting policy (PRD-001 FR-ORG-10, FR-PPL-07)

### Requirement: NM-LV-10 Flexible work requests
Formal flexible-work requests SHALL be tracked with the 2-month written reply deadline, a reason for any refusal, and the reply kept.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Deadline
- **WHEN** an employee submits a formal flexible-work request on 1 Oct
- **THEN** the reply is due by 1 Dec, and Checks shows ⏰ if it is not sent by then (N.1 #5)

#### Scenario: Refusal
- **WHEN** the request is refused
- **THEN** the written reply gives the business reason

#### Scenario: Receipt date kept
- **GIVEN** a formal request received on Thu 1 Oct
- **WHEN** HR asks a clarifying question or changes the request's type
- **THEN** the written reply is still due by Tue 1 Dec, and it records the business grounds

### Requirement: NM-LV-11 Counting basis per leave arrangement
Each leave type and arrangement SHALL state its counting basis (calendar days or weeks, working days by the person's pattern, or hours) and what happens when a holiday or other leave falls inside it, and SHALL keep the arrangement chosen and the original entitlement.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Continuous maternity leave
- **GIVEN** an employee takes 16 weeks of maternity leave in one block from Mon 4 Jan 2027
- **WHEN** it is counted
- **THEN** it lasts 112 calendar days, holidays and rest days included

#### Scenario: Flexible portion
- **GIVEN** the employee agrees 8 weeks in one block and 8 weeks flexibly within 12 months of the birth
- **WHEN** a flexible day is taken
- **THEN** it counts in working days by her pattern, holidays and rest days excluded

#### Scenario: A holiday inside annual leave
- **GIVEN** annual leave from Wed 27 Oct to Fri 29 Oct 2027
- **WHEN** it is counted
- **THEN** 2 days are deducted, because Deepavali, Thu 28 Oct, is not
- **AND** Fyniq's available hours subtract the holiday once

### Requirement: NM-LV-12 Sick leave rules
Outpatient and hospitalisation leave SHALL share one ceiling and step up at 3, 4, 5 and 6 months' service; sick leave SHALL be notified within 48 hours rather than applied for; HR SHALL verify the certificate, and a manager SHALL NOT refuse a certified day the person is entitled to.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Shared ceiling
- **GIVEN** an employee with 6 months' service who has used 10 outpatient days
- **WHEN** they are hospitalised
- **THEN** 50 hospitalisation days remain

#### Scenario: Service threshold
- **GIVEN** an employee with 4 completed months' service
- **WHEN** their sick leave is checked
- **THEN** 8 outpatient and 30 hospitalisation days apply

#### Scenario: Notified after the event
- **GIVEN** an employee who informs their manager within 48 hours and gives the certificate on return
- **WHEN** the absence is recorded
- **THEN** it is accepted without an advance approval

#### Scenario: Consultation fees
- **WHEN** a consultation fee must be reimbursed
- **THEN** it goes to Fyniq's claims (PRD-001 FR-AP-17) or a recorded manual route [verify the scope]

### Requirement: NM-LV-13 Leave ledger
Every leave balance change SHALL be a ledger entry with its leave year and source; a pending request SHALL reserve its days in the same transaction as the balance check; intervals SHALL NOT overlap; cancellations SHALL return days to their year; changes after an approved pay run SHALL be next-run inputs.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Two requests for the last day
- **GIVEN** 1 day of annual leave left
- **WHEN** two one-day requests are submitted at the same moment
- **THEN** one reserves the day and the other is refused

#### Scenario: Cancellation returns to its year
- **GIVEN** days reserved from 2026's entitlement
- **WHEN** the request is cancelled in January 2027
- **THEN** the days return to 2026's entitlement, and expire with it

#### Scenario: After payroll
- **GIVEN** unpaid leave already used by the approved October run
- **WHEN** it is corrected
- **THEN** the change is an input to the November run, and the October run is not edited

### Requirement: NM-LV-14 Government-paid leave per scheme
Each government-paid scheme SHALL keep its own allocation versions, unit, period, reimbursement cap and claim deadline from the pack, and each claim SHALL record days, amount, partial receipts and rejections, without the salary owed depending on it.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: A sharing change
- **GIVEN** parents of a child born on 10 Apr 2026 share 10 weeks of shared parental leave, half each
- **WHEN** they change the split in week 6, with the employer's written agreement
- **THEN** a new allocation version is kept
- **AND** no leave or reimbursement is counted twice

#### Scenario: Caps by scheme
- **WHEN** shared parental leave is claimed
- **THEN** the cap is S$2,500 a week
- **AND** for childcare days 4–6 the cap is S$500 a day

#### Scenario: Deadlines by scheme
- **WHEN** the last day of shared parental leave is 30 Jun 2027
- **THEN** its claim is due by 30 Sep 2027
- **AND** childcare leave taken in 2026 is claimed by 31 Mar 2027

#### Scenario: Partial or rejected claim
- **WHEN** a claim is partly paid or rejected
- **THEN** the outstanding amount stays open, and the employee's salary is unchanged

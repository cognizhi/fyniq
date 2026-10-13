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

### Requirement: NM-LV-02 Entitlement and balance
Each person's balance per type SHALL show entitled, taken, booked and left, from accrual or grant, proration, statutory minimums, carry-forward and encashment rules.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Balance shown
- **WHEN** an employee opens their annual leave
- **THEN** they see entitled, taken, booked and left, and can see how it was calculated

### Requirement: NM-LV-03 Apply in under a minute
The leave form SHALL show the balance after the request, exclude public holidays and rest days, show who else is away, warn on a clash and refuse a request beyond the balance unless the type allows it.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Holidays excluded
- **WHEN** an employee asks for Tue 10 Nov to Fri 13 Nov 2026
- **THEN** 4 days are counted, and Mon 9 Nov (Deepavali observed [verify]) is not part of the request

#### Scenario: Beyond the balance
- **WHEN** the request is more than the balance and the type does not allow a negative balance
- **THEN** it is refused with the balance

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

### Requirement: NM-LV-06 Medical certificates
Sick leave SHALL record its certificate and issuer, HR SHALL verify it, and medical details SHALL be restricted.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Verify
- **WHEN** an employee records sick leave with a certificate photo
- **THEN** HR sees it to verify, and other roles do not

### Requirement: NM-LV-07 Government-paid leave
Nemura SHALL record the government-paid portion of maternity, paternity, shared parental, childcare, extended childcare and adoption leave and track the employer's reimbursement claim, filed by a person on the Government-Paid Leave portal.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Claim tracked
- **WHEN** paternity leave is taken
- **THEN** the government-paid amount and the claim's date, status and evidence are tracked

#### Scenario: Before the deadline
- **WHEN** a claim is not filed 30 days before its deadline
- **THEN** Checks shows ⏰ (N.1 #10)

### Requirement: NM-LV-08 Leave into pay
Unpaid leave and leave encashment SHALL become pay-run inputs; this supersedes PRD-001 FR-PAY-12.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Unpaid leave
- **WHEN** an employee takes 2 days of unpaid leave in a month
- **THEN** the pay run includes the deduction from Nemura's leave record

### Requirement: NM-LV-09 Leave into the books
Leave taken SHALL update available hours in Fyniq, and an optional month-end leave liability accrual SHALL go to Fyniq as a journal for approval.

Source: PRD-002 §8 · Priority S · Phase P1 / P2 (accrual)

#### Scenario: Available hours
- **WHEN** leave is approved and taken
- **THEN** Fyniq's available hours for that person drop by those hours (PRD-001 FR-PRJ-12)

### Requirement: NM-LV-10 Flexible work requests
Formal flexible-work requests SHALL be tracked with the 2-month written reply deadline, a reason for any refusal, and the reply kept.

Source: PRD-002 §8 · Priority M · Phase P1

#### Scenario: Deadline
- **WHEN** an employee submits a formal flexible-work request on 1 Oct
- **THEN** the reply is due by 1 Dec, and Checks shows ⏰ if it is not sent by then (N.1 #5)

#### Scenario: Refusal
- **WHEN** the request is refused
- **THEN** the written reply gives the business reason

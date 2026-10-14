# people/checks Specification

## Purpose
The checks Nemura runs, each shown on Nemura's Checks with its status. Derived from PRD-002 Appendix C (N.1).

## Requirements

### Requirement: N.1 #1 Missing KETs
The system SHALL block a person who started 14 or more days ago with no issued KETs (NM-LTR-06).

Source: PRD-002 §N.1

#### Scenario: Missing KETs
- **WHEN** a person started 14 days ago and has no issued KETs
- **THEN** the check shows ✖

### Requirement: N.1 #2 Pay run late
The system SHALL flag as due a pay run not approved by its latest approval date, or salary or overtime that would be paid later than the pack allows (NM-PAY-03).

Source: PRD-002 §N.1

#### Scenario: Late approval
- **WHEN** a pay run is not approved by its latest approval date
- **THEN** the check shows ⏰

### Requirement: N.1 #3 Work pass expiring
The system SHALL block a work pass expiring within 30 days with no renewal or leaving date, or already expired (NM-PPL-03).

Source: PRD-002 §N.1

#### Scenario: No renewal
- **WHEN** a pass expires in 20 days with no renewal and no leaving date
- **THEN** the check shows ✖

### Requirement: N.1 #4 Leave balance or approval overdue
The system SHALL flag for review a balance below zero for a type that does not allow it, or a leave request waiting more than 3 working days for its approver (NM-LV-02, NM-LV-04).

Source: PRD-002 §N.1

#### Scenario: Waiting
- **WHEN** a leave request has waited 4 working days
- **THEN** the check shows ⚠

### Requirement: N.1 #5 Request reply overdue
The system SHALL flag as due a flexible-work request without a written reply within 2 months, or a letter request past its due date (NM-LV-10, NM-ESS-04).

Source: PRD-002 §N.1

#### Scenario: Flexible work
- **WHEN** a formal flexible-work request is 2 months old with no written reply
- **THEN** the check shows ⏰

### Requirement: N.1 #6 Pay run and books differ
The system SHALL block an approved pay run whose totals differ from what the books received, or whose hand-off has not completed by pay day (NM-PAY-05, NM-INT-03).

Source: PRD-002 §N.1

#### Scenario: Differ
- **WHEN** Fyniq's posting totals differ from the approved run's
- **THEN** the check shows ✖

### Requirement: N.1 #7 Fyniq link not syncing
The system SHALL flag for review a Fyniq link that has not synced successfully for 24 hours (NM-INT-04).

Source: PRD-002 §N.1

#### Scenario: Not syncing
- **WHEN** the last successful sync was 25 hours ago
- **THEN** the check shows ⚠

### Requirement: N.1 #8 Pack not ready
The system SHALL flag for review next year's holiday calendar not loaded by 1 Dec (NM-PACK-05); a [verify] pack value used by a live policy moved to N.1 #13 as a block.

Source: PRD-002 §N.1

#### Scenario: Calendar missing
- **WHEN** next year's calendar is not loaded on 1 Dec
- **THEN** the check shows ⚠

### Requirement: N.1 #9 Payslip late
The system SHALL block a payslip not published within 3 working days of the salary payment (NM-PAY-04).

Source: PRD-002 §N.1

#### Scenario: Late payslip
- **WHEN** a payslip is unpublished 4 working days after the salary payment
- **THEN** the check shows ✖

### Requirement: N.1 #10 Government-paid leave claim due
The system SHALL flag as due a government-paid leave reimbursement claim not filed 30 days before its deadline (NM-LV-07).

Source: PRD-002 §N.1

#### Scenario: Claim due
- **WHEN** a claim's deadline is 29 days away and it is not filed
- **THEN** the check shows ⏰

### Requirement: N.1 #11 Candidate data past retention
The system SHALL flag for review unsuccessful candidates' data past its retention period without consent to keep it (NM-REC-05).

Source: PRD-002 §N.1

#### Scenario: Past retention
- **WHEN** a candidate's data is 7 months old with no consent to keep it
- **THEN** the check shows ⚠

### Requirement: N.1 #12 Records waiting for deletion
The system SHALL flag for review records past their retention date waiting for the reviewed deletion job, and SHALL block once their review date has passed (NM-SEC-05).

Source: PRD-002 §N.1

#### Scenario: Waiting
- **WHEN** records are past their retention date and not yet deleted
- **THEN** the check shows ⚠

#### Scenario: Review date passed
- **WHEN** records are still waiting after their review date
- **THEN** the check shows ✖

### Requirement: N.1 #13 Unverified pack value in use
The system SHALL block a live calculation that needs a pack value that is not verified and has no recorded company decision (NM-PACK-07).

Source: PRD-002 §N.1

#### Scenario: Unverified value
- **WHEN** an employee applies for unpaid infant care leave and its entitlement is still researched, with no company decision
- **THEN** the check shows ✖

### Requirement: N.1 #14 Salary payment not paid
The system SHALL block on a salary payment that is rejected, returned or whose outcome is unknown (NM-PAY-10).

Source: PRD-002 §N.1

#### Scenario: Rejected payment
- **WHEN** the bank rejects one person's salary payment
- **THEN** the check shows ✖ until it is paid or resolved

### Requirement: N.1 #15 Final pay or notification due
The system SHALL flag as due a final salary, final payslip or MOM retrenchment notification due within 3 working days or late (NM-PAY-08).

Source: PRD-002 §N.1

#### Scenario: Final pay due
- **WHEN** a leaver's final salary is due in 2 working days
- **THEN** the check shows ⏰

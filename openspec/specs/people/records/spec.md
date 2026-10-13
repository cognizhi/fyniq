# people/records Specification

## Purpose
People records in Nemura: the record and its effective-dated history, restricted fields, work passes, high-risk bank changes, documents and the directory. Derived from PRD-002 §7.

## Requirements

### Requirement: NM-PPL-01 People record
Nemura SHALL keep each person's record with effective-dated history of job, pay, manager and department, extending PRD-001 FR-EMP-01, whose fields it now owns.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: History
- **WHEN** a person's job title changes from 1 Nov
- **THEN** the record shows the new title from 1 Nov and keeps the old one as history

### Requirement: NM-PPL-02 Restricted fields
NRIC or FIN, pay, bank account, medical and disciplinary data SHALL be visible only to roles with that right, masked by default, with every read audited.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Masked by default
- **WHEN** an HR officer opens a person's record
- **THEN** the NRIC or FIN is masked, and revealing it is audited (AUD-05)

### Requirement: NM-PPL-03 Work passes
Nemura SHALL record each person's work pass and its dates, remind at 90, 60 and 30 days before expiry, and flag a pass ending without a renewal or a leaving date.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Reminder
- **WHEN** an Employment Pass expires in 60 days
- **THEN** the HR officer is reminded

#### Scenario: No plan
- **WHEN** a pass expires within 30 days with no renewal or leaving date
- **THEN** Checks shows ✖ (N.1 #3)

### Requirement: NM-PPL-04 Salary bank account changes are high-risk
A salary bank account change SHALL take effect only after a different HR or Payroll administrator approves it and the employee confirms it with a passkey, and all payroll approvers SHALL be notified.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Proposed by the employee
- **WHEN** an employee proposes a new bank account
- **THEN** it applies only after another administrator approves it and the employee confirms with a passkey
- **AND** all payroll approvers are notified and the next pay run shows the change

### Requirement: NM-PPL-05 Documents per person
Each person's documents SHALL have a type, a retention rule and an audience, and SHALL be kept as evidence where the type requires it.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Contract stored
- **WHEN** a signed contract is uploaded
- **THEN** it is kept as evidence with the contract retention rule, visible only to its audience

### Requirement: NM-PPL-06 Directory and org chart
Everyone SHALL see names, job titles, departments, work emails and the reporting line, and nothing restricted.

Source: PRD-002 §7 · Priority S · Phase P1

#### Scenario: Directory
- **WHEN** an employee opens the directory
- **THEN** they see colleagues' names, titles, departments, work emails and managers, and no restricted field

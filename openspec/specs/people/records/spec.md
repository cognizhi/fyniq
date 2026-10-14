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

#### Scenario: Legal classification
- **WHEN** an employment type is recorded
- **THEN** it is the legal classification of NM-PPL-08

### Requirement: NM-PPL-02 Restricted fields
NRIC or FIN, pay, bank account, medical and disciplinary data SHALL be visible only to roles with that right, masked by default, with every read audited.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Masked by default
- **WHEN** an HR officer opens a person's record
- **THEN** the NRIC or FIN is masked, and revealing it is audited (AUD-05)

### Requirement: NM-PPL-03 Work passes
Nemura SHALL record each person's work authorisation (pass type, issuer, restrictions, validity and state), remind at 90, 60 and 30 days before expiry, and flag a pass ending without a renewal or a leaving date.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Reminder
- **WHEN** an Employment Pass expires in 60 days
- **THEN** the HR officer is reminded

#### Scenario: No plan
- **WHEN** a pass expires within 30 days with no renewal or leaving date
- **THEN** Checks shows ✖ (N.1 #3)

#### Scenario: Dependant's Pass holder
- **GIVEN** a Dependant's Pass holder with a Letter of Consent and no work pass
- **WHEN** HR checks whether they may work for the company
- **THEN** Nemura does not mark them cleared: they need an Employment Pass, S Pass or DP-Work Permit (MOM)

#### Scenario: Applied is not issued
- **WHEN** a renewal is applied for but not yet issued
- **THEN** it is not shown as a valid pass

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

### Requirement: NM-PPL-07 Dependants for leave
For family leave, Nemura SHALL keep each child's date of birth or adoption intent, citizenship and relationship, with declarations and evidence, restricted, and SHALL work out eligibility per scheme and period, showing why and from when a person is not eligible.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Two children, one cap
- **GIVEN** a parent with two Singapore Citizen children under 7
- **WHEN** childcare leave is granted for 2026
- **THEN** they get 6 days for the year, not 12

#### Scenario: Non-citizen child
- **GIVEN** a parent of a non-citizen child under 7, with 3 months' service
- **WHEN** entitlements are worked out
- **THEN** they get 2 days of employer-paid childcare leave a year

#### Scenario: A fact changes
- **WHEN** a child's citizenship changes on 1 Jun
- **THEN** only the periods from 1 Jun are re-evaluated

#### Scenario: Limits per child and older children
- **WHEN** childcare leave is worked out over the years for one child
- **THEN** a Singapore Citizen child gives at most 42 days in all, and a non-citizen child at most 14
- **AND** for a child aged 7–12 the parent gets 2 days of extended childcare leave a year (Appendix A)

### Requirement: NM-PPL-08 Legal classification per person
Nemura SHALL keep each person's effective-dated legal classification (contract type, Employment Act and Part IV coverage, residency, retirement cohort), recorded by HR with evidence where it cannot be derived, and rules SHALL read it rather than the worksite alone.

Source: PRD-002 §7 · Priority M · Phase P1

#### Scenario: Contractor
- **GIVEN** a contractor on a contract for service
- **WHEN** leave entitlements are worked out
- **THEN** none of the statutory employee entitlements applies

#### Scenario: Part IV
- **GIVEN** a non-workman earning S$2,400 basic a month
- **WHEN** they are classified
- **THEN** they are covered by Part IV (non-workman threshold S$2,600)

#### Scenario: Retirement cohort
- **GIVEN** a citizen born on 15 Aug 1962
- **WHEN** their retirement age is worked out
- **THEN** it is 63 (born 1 Jul 1960–30 Jun 1963)

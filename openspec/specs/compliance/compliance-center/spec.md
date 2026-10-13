# compliance/compliance-center Specification

## Purpose
The in-app Compliance Center of Fyniq: the control dashboard, the statutory calendar and its reminders, manual attestations, evidence export, the PDPA registers, the risk register and the policy library. Derived from PRD-001 §14.7; compliance as code from PRD-001 §14.1.

## Requirements

### Requirement: CMP-01 Control dashboard
The Compliance Center SHALL show every control in the registry with its status, last run, evidence link, owner and framework mappings, with filters by framework.

Source: PRD-001 §14.7 · Priority M · Phase P1

#### Scenario: Control statuses
- **WHEN** the control dashboard is shown
- **THEN** each control's status is one of pass, fail, overdue, not applicable or not assessed
- **AND** each control shows its last run, evidence link, owner and framework mappings

#### Scenario: Not assessed names the missing input
- **WHEN** a control cannot be assessed because a required input is missing
- **THEN** its status is not assessed
- **AND** the missing input is named

#### Scenario: Filter by framework
- **WHEN** the user filters the dashboard by a framework
- **THEN** only the controls mapped to that framework are shown

### Requirement: CMP-02 Statutory calendar
The Compliance Center SHALL generate a statutory calendar from the organisation's FY, GST periods, payroll and filing categories, with reminders and overdue items on Checks.

Source: PRD-001 §14.7 · Priority M · Phase P1

#### Scenario: Deadlines in the calendar
- **WHEN** the statutory calendar is generated
- **THEN** it holds GST F5 due dates, CPF (14th), AIS (1 Mar), ECI (FY end + 3 months), Form C-S/C (30 Nov), ACRA annual return (FY end + 7 months), AGM and register-of-controllers updates

#### Scenario: Share-issue approvals and returns of allotment
- **WHEN** the statutory calendar is generated
- **THEN** it holds share-issue approvals (lodged within 14 days of the resolution) and pending returns of allotment (FR-EQ-09)

#### Scenario: Calendar follows the organisation's settings
- **WHEN** the statutory calendar is generated
- **THEN** its items and due dates are computed from the organisation's FY, GST periods, payroll and filing categories

#### Scenario: Reminders
- **WHEN** a calendar item is 30, 14, 7 or 1 days from its due date
- **THEN** a reminder is sent

#### Scenario: Overdue items on Checks
- **WHEN** a calendar item passes its due date undone
- **THEN** it appears on Checks as overdue

### Requirement: CMP-03 Manual attestations
The Compliance Center SHALL record manual attestations (access review, journal review, close sign-off, policy acknowledgement) with due dates, sign-off and evidence.

Source: PRD-001 §14.7 · Priority M · Phase P1

#### Scenario: Attestation recorded
- **WHEN** an access review, journal review, close sign-off or policy acknowledgement is attested
- **THEN** it is recorded with its due date, its sign-off and its evidence

### Requirement: CMP-04 Evidence export
The Compliance Center SHALL export evidence per framework and period for auditors and certification bodies.

Source: PRD-001 §14.7 · Priority S · Phase P2

#### Scenario: Export for a framework and period
- **WHEN** an evidence export is requested for a framework and a period
- **THEN** the evidence of that framework's controls for that period is exported for auditors and certification bodies

### Requirement: CMP-05 PDPA registers
The Compliance Center SHALL hold the data inventory, the DPIAs and a breach register with PDPA timers (CMP-PD-02/05/08).

Source: PRD-001 §14.7 · Priority M · Phase P1

#### Scenario: Data inventory and DPIAs
- **WHEN** the Compliance Center is opened
- **THEN** the data inventory (CMP-PD-02) and the DPIAs (CMP-PD-08) are available in it

#### Scenario: Breach register with PDPA timers
- **WHEN** a breach is entered in the breach register
- **THEN** its PDPA timers (CMP-PD-05) run from it

### Requirement: CMP-06 Risk register
The Compliance Center SHALL keep a risk register (R1–R19 and new risks) with owners and review dates.

Source: PRD-001 §14.7 · Priority S · Phase P1

#### Scenario: Risks with owners and review dates
- **WHEN** the risk register is shown
- **THEN** it holds risks R1–R19 and any new risks
- **AND** each risk has an owner and a review date

### Requirement: CMP-07 Policy library
The Compliance Center SHALL keep a policy library with versions and staff acknowledgements.

Source: PRD-001 §14.7 · Priority S · Phase P1

#### Scenario: Policies held
- **WHEN** the policy library is shown
- **THEN** it holds the information security, acceptable use, access control, incident response, backup, change management, AI use and data protection policies

#### Scenario: Versions and acknowledgements
- **WHEN** a policy is published in a new version
- **THEN** the version is kept
- **AND** staff acknowledgements are recorded against it

### Requirement: Control registry as code
Every control SHALL live in the repository's control registry (`compliance/controls/*.yaml`) with its ID, description, owner, framework mappings, the automated check that proves it, its frequency and the evidence it produces.

Source: PRD-001 §14.1

#### Scenario: Control entry is complete
- **WHEN** a control is added to the registry
- **THEN** it carries an ID, description, owner, framework mappings (Singapore statute, PDPA, ISO/IEC 27001:2022 Annex A, SOC 2 Trust Services Criteria, CSA Cyber Essentials / Cyber Trust, IMDA agentic-AI framework, OWASP), its automated check, frequency and evidence

#### Scenario: Appendix F seeds the registry
- **WHEN** the registry is first created
- **THEN** it holds the controls of PRD Appendix F

### Requirement: Checks run where the risk is
Control checks SHALL run in CI, at runtime, against the cloud configuration, and as scheduled manual attestations recorded in the platform.

Source: PRD-001 §14.1

#### Scenario: Where each kind of check runs
- **WHEN** controls are checked
- **THEN** code, infrastructure plans, database grants and AI evals are checked in CI
- **AND** the nightly ledger and audit-chain verification and the deadline monitors run at runtime
- **AND** organisation policies, Security Command Center findings and log sinks are checked in the cloud configuration
- **AND** access reviews and journal reviews are scheduled manual attestations recorded in the platform

### Requirement: Evidence collected automatically
Evidence SHALL be collected automatically into the retention-locked `fyniq-audit` project, time-stamped and linked to the control and to the commit or run that produced it.

Source: PRD-001 §14.1

#### Scenario: Evidence from a check run
- **WHEN** a control check runs
- **THEN** its evidence is stored in the `fyniq-audit` project with a time stamp
- **AND** it is linked to the control and the commit or run that produced it

### Requirement: The Compliance Center shows controls and raises tasks
The Compliance Center SHALL show every control's status, last run, evidence and owner, plus the statutory calendar, and SHALL raise Inbox tasks when anything fails or falls due.

Source: PRD-001 §14.1

#### Scenario: Control status at a glance
- **WHEN** a user with access opens the Compliance Center
- **THEN** each control shows its status, last run, evidence and owner, next to the statutory calendar

#### Scenario: Failure or due date raises an Inbox task
- **WHEN** a control fails or a statutory item falls due
- **THEN** an Inbox task is raised for its owner

### Requirement: Failing controls block releases or page the owner
A failing preventive control SHALL block releases, and a failing detective control SHALL page its owner.

Source: PRD-001 §14.1

#### Scenario: Preventive control fails
- **GIVEN** a preventive control, for example a database grant test
- **WHEN** it fails
- **THEN** the release is blocked

#### Scenario: Detective control fails
- **GIVEN** a detective control, for example an overdue CPF payment
- **WHEN** it fails
- **THEN** the control's owner is paged


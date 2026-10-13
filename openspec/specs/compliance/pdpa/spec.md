# compliance/pdpa Specification

## Purpose
How Fyniq meets the Personal Data Protection Act: the Data Protection Officer, the personal-data inventory, notices and consent, access and correction requests, breach response timers, transfer limitation, NRIC handling, impact assessments, penalty awareness and the Data Protection Trustmark. Derived from PRD-001 §14.3.

## Requirements

### Requirement: CMP-PD-01 Data Protection Officer
A Data Protection Officer SHALL be appointed, and their contact and the data protection policies SHALL be published.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: DPO appointed and contact published
- **WHEN** someone looks for who handles personal data at the company
- **THEN** an appointed Data Protection Officer is named
- **AND** the DPO's contact is published

#### Scenario: Data protection policies published
- **WHEN** someone looks for the data protection policies
- **THEN** they are published

### Requirement: CMP-PD-02 Personal-data inventory and retention schedule
The system SHALL maintain a personal-data inventory and a retention schedule in the Compliance Center.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Inventory entry contents
- **WHEN** personal data is recorded in the inventory
- **THEN** the entry states what is held, why, where, who has it, how long it is kept, and its cross-border flows, including AI model calls

#### Scenario: Retention schedule maintained
- **WHEN** the inventory is maintained in the Compliance Center
- **THEN** the retention schedule is maintained with it

### Requirement: CMP-PD-03 Notices, purposes and consent
The system SHALL give notices and purposes to employees, contacts and (P3) customers' users, and SHALL obtain consent where required.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Notices for employees and contacts
- **WHEN** personal data of an employee or a contact is collected
- **THEN** the notice and its purposes are given to that person

#### Scenario: Notices for customers' users
- **GIVEN** Fyniq is offered to customers (P3)
- **WHEN** personal data of a customer's user is collected
- **THEN** the notice and its purposes are given to that user

#### Scenario: Consent where required
- **WHEN** a purpose requires consent
- **THEN** consent is obtained for it

### Requirement: CMP-PD-04 Access and correction requests
Access and correction requests SHALL be handled in a tracked workflow and answered within 30 days, or the requester SHALL be told in writing within 30 days when it will take longer.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Request tracked
- **WHEN** an access or correction request is received
- **THEN** it is tracked in a workflow until it is answered

#### Scenario: Answered within 30 days
- **WHEN** an access or correction request can be answered in time
- **THEN** it is answered within 30 days

#### Scenario: Written notice when it takes longer
- **WHEN** an access or correction request will take longer than 30 days
- **THEN** the requester is told in writing within 30 days when it will be answered

### Requirement: CMP-PD-05 Breach response timers
The system SHALL time breach responses: assessment within 30 days of becoming aware, and notification of the PDPC within 3 calendar days after deciding a breach is notifiable.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Assessment within 30 days
- **WHEN** the company becomes aware of a possible data breach
- **THEN** the assessment timer runs for 30 days

#### Scenario: Notifiable breach reported to the PDPC
- **WHEN** a breach is decided to be notifiable (significant harm or ≥ 500 individuals)
- **THEN** the PDPC is notified within 3 calendar days after that decision

#### Scenario: Prescribed data is notifiable
- **WHEN** a leak includes a name or NRIC with salary or bank details
- **THEN** the breach is treated as notifiable (prescribed data)

#### Scenario: Data intermediary notifies customers
- **GIVEN** Fyniq acts as a data intermediary for customers (P3)
- **WHEN** a breach affects a customer's data
- **THEN** the customer is notified without undue delay

### Requirement: CMP-PD-06 Transfer limitation
Every overseas transfer (AI endpoint under ADR-T2 Option B/C, DR copies, sub-processors) SHALL be covered by legally enforceable obligations (contracts, binding rules) or certification, assessed and recorded.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Overseas transfer covered and recorded
- **WHEN** personal data is transferred overseas, to an AI endpoint under ADR-T2 Option B/C, as a DR copy or to a sub-processor
- **THEN** the transfer is covered by legally enforceable obligations (contracts, binding rules) or certification
- **AND** the transfer has been assessed and the assessment is recorded

### Requirement: CMP-PD-07 NRIC handling
NRIC SHALL be collected only where the law requires it (CPF, IRAS), encrypted (SEC-11), partially masked in the UI [verify current masking guidance], and never used for authentication or as a file password.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: Collected only where the law requires it
- **WHEN** NRIC would be collected
- **THEN** it is collected only where the law requires it (CPF, IRAS)

#### Scenario: Encrypted and partially masked
- **WHEN** an NRIC is stored and shown in the UI
- **THEN** it is encrypted (SEC-11)
- **AND** it is partially masked in the UI [verify current masking guidance]

#### Scenario: Never used for authentication
- **WHEN** a user or a document recipient authenticates
- **THEN** the NRIC is never used for authentication

#### Scenario: Never used as a file password
- **WHEN** a file such as a payslip PDF is protected
- **THEN** the NRIC is never used as its password, in line with the PDPC/CSA advisory with phase-out by 31 Dec 2026

### Requirement: CMP-PD-08 Data protection impact assessments
A data protection impact assessment SHALL be done for payroll, the AI assistant and each new processing of personal data.

Source: PRD-001 §14.3 · Priority M · Phase P1

#### Scenario: DPIA for payroll and the AI assistant
- **WHEN** payroll or the AI assistant processes personal data
- **THEN** a data protection impact assessment covers it

#### Scenario: DPIA for new processing
- **WHEN** a new processing of personal data is introduced
- **THEN** a data protection impact assessment is done for it

### Requirement: CMP-PD-09 Penalty awareness
The risk register SHALL record awareness of the PDPA penalty: up to 10% of Singapore turnover above S$10m, otherwise S$1m.

Source: PRD-001 §14.3 · Priority S · Phase P1

#### Scenario: Penalty in the risk register
- **WHEN** the risk register is reviewed
- **THEN** it records the PDPA penalty of up to 10% of Singapore turnover when turnover is above S$10m, otherwise up to S$1m

### Requirement: CMP-PD-10 Data Protection Trustmark
The company SHALL obtain the Data Protection Trustmark (IMDA; valid 3 years) when selling to customers who ask for it.

Source: PRD-001 §14.3 · Priority C · Phase P3

#### Scenario: Trustmark when customers ask
- **GIVEN** a customer asks for the Data Protection Trustmark
- **WHEN** Fyniq is sold to that customer
- **THEN** the company holds the IMDA Data Protection Trustmark, valid for 3 years

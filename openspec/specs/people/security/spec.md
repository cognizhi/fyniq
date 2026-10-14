# people/security Specification

## Purpose
Security, privacy and records in Nemura: inherited controls, need to know, PDPA, records the law requires, retention and grievances. Derived from PRD-002 §20.

## Requirements

### Requirement: NM-SEC-01 Inherited controls
Nemura SHALL apply PRD-001's security, audit and tenancy requirements with its own threat model, and changes to the AI write path, approvals, pay or bank details, tenancy or the audit trail SHALL need a second human reviewer.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Tenancy
- **WHEN** any Nemura table is created
- **THEN** it has `organisation_id` and forced row-level security (PRD-001 §11.4)

#### Scenario: ASVS Level 3 where money moves
- **WHEN** pay, bank-detail and approval flows are reviewed
- **THEN** ASVS Level 3 applies to their business logic, authentication, authorisation and logging

### Requirement: NM-SEC-02 Need to know
Pay, NRIC or FIN, bank, medical, disciplinary, review and candidate data SHALL each need a right per field and action, be masked by default and have every read audited; a person SHALL see what is routinely released to them, and the rest through an access request.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Own data
- **WHEN** an employee opens their own profile
- **THEN** they see their own routinely released fields, and the read is audited

#### Scenario: A file that names others
- **GIVEN** a disciplinary file that names a complainant
- **WHEN** the employee asks to see it
- **THEN** it goes through an access request (NM-SEC-07)

### Requirement: NM-SEC-03 PDPA
Nemura SHALL record purposes per record type, handle access and correction requests with a due date, list each type in the data inventory and assess transfers outside Singapore.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Access request
- **WHEN** a candidate asks for their data
- **THEN** the request has a due date and its handling is recorded

### Requirement: NM-SEC-04 Records the law requires
Employment records and payslips SHALL be kept for at least the pack's period (Singapore: the latest 2 years for current employees; the last 2 years, for 1 year after leaving, for former employees), and KETs and letters SHALL be kept as evidence.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Leaver's payslips
- **WHEN** a person leaves
- **THEN** the last 2 years of their payslips and employment records are kept for at least 1 year after they leave

#### Scenario: A minimum, not a deletion date
- **WHEN** another law, a purpose or a legal hold needs a record longer
- **THEN** it is kept longer

### Requirement: NM-SEC-05 Retention per record type
Each record type SHALL have a retention rule with its reason, due deletions SHALL run as a reviewed job with a report and a review date after which they are ✖, each deletion SHALL leave proof, and data under a legal hold SHALL be kept.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Reviewed deletion
- **WHEN** records pass their retention date
- **THEN** they wait for the reviewed deletion job, never silently deleted, and Checks shows ⚠ while they wait (N.1 #12)
- **AND** ✖ once the review date (default 30 days) has passed

#### Scenario: Default retention
- **WHEN** no company policy overrides the pack
- **THEN** candidate data is kept 6 months and a medical certificate 1 year after the leave

#### Scenario: Proof of deletion
- **WHEN** records are deleted
- **THEN** proof of what went and when is kept

### Requirement: NM-SEC-06 Grievances
Nemura SHALL offer a confidential grievance channel with an inquiry record, a written outcome, restricted access and another handler when the default handler is the subject, ready before the Workplace Fairness Act commences.

Source: PRD-002 §20 · Priority S · Phase P2

#### Scenario: Confidential
- **WHEN** an employee raises a grievance
- **THEN** only the people handling it can see it, and the outcome is given in writing

#### Scenario: Small firms
- **GIVEN** a company with fewer than 25 employees, exempt from the Act to start with (MOM, 4 Nov 2025)
- **WHEN** the grievance channel is set up
- **THEN** it is still offered, under the Tripartite Guidelines

#### Scenario: Complaint about the handler
- **GIVEN** a grievance about the HR officer
- **WHEN** it is raised
- **THEN** it goes to another authorised person, and the HR officer cannot see it

### Requirement: NM-SEC-07 Access to one's own file
Own-file requests SHALL be tracked with a due date, reviewed, cleared of other people's personal data with each exception recorded, and answered in writing; withdrawing consent SHALL never delete records the law requires.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Peer feedback
- **GIVEN** a review containing a peer's private feedback
- **WHEN** the employee asks for their file
- **THEN** they get it with the peer's personal data removed, and the exception recorded

#### Scenario: Evaluative opinions
- **WHEN** a candidate asks for the opinions formed about them
- **THEN** opinions kept solely for evaluation are withheld, and the exception is recorded (PDPC Selected Topics guidelines §6.10, revised 23 May 2024)

#### Scenario: Withdrawal of consent
- **WHEN** an employee withdraws consent
- **THEN** records the law requires are kept

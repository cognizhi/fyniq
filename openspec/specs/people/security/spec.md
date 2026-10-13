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

### Requirement: NM-SEC-02 Need to know
Pay, NRIC or FIN, bank, medical, disciplinary, review and candidate data SHALL each need a right, be masked by default and have every read audited, and a person SHALL always see their own.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Own data
- **WHEN** an employee opens their own profile
- **THEN** they see their own restricted fields, and the read is audited

### Requirement: NM-SEC-03 PDPA
Nemura SHALL record purposes per record type, handle access and correction requests with a due date, list each type in the data inventory and assess transfers outside Singapore.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Access request
- **WHEN** a candidate asks for their data
- **THEN** the request has a due date and its handling is recorded

### Requirement: NM-SEC-04 Records the law requires
Employment records and payslips SHALL be kept for at least the pack's period, and KETs and letters kept as evidence.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Leaver's payslips
- **WHEN** a person leaves
- **THEN** their payslips and employment records are kept for at least the Singapore pack's period

### Requirement: NM-SEC-05 Retention per record type
Each record type SHALL have a retention rule, due deletions SHALL run as a reviewed job with a report, and data under a legal hold SHALL be kept.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Reviewed deletion
- **WHEN** records pass their retention date
- **THEN** they wait for the reviewed deletion job, never silently deleted, and Checks shows ⚠ while they wait (N.1 #12)

### Requirement: NM-SEC-06 Grievances
Nemura SHALL offer a confidential grievance channel with an inquiry record, a written outcome and restricted access, ready before the Workplace Fairness Act commences.

Source: PRD-002 §20 · Priority S · Phase P2

#### Scenario: Confidential
- **WHEN** an employee raises a grievance
- **THEN** only the people handling it can see it, and the outcome is given in writing

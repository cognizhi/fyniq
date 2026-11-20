# people/security Specification

## Purpose
Security, privacy and records in Nemura: inherited controls, need to know, PDPA, records the law requires, retention and grievances. Derived from PRD-002 §20.

## Requirements

### Requirement: NM-SEC-01 Inherited controls
Nemura SHALL apply PRD-001's security, audit and tenancy requirements with its own threat model, and changes to the AI write path, approvals, pay or bank details, tenancy or the audit trail SHALL need a second human reviewer.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: A person reviews the report (rev 0.73)
- **WHEN** the nightly job has listed records due for deletion and an Owner or an HR administrator reviews the report
- **THEN** the report shows the kind, how many are due and held and the review date, names no person, is reviewed once, and only then does the next nightly job delete what is still due

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

### Requirement: NM-SEC-08 Erasure of a former employee's personal data
An Owner or an HR administrator SHALL be able to ask for a former employee's personal data to be erased once the retention periods have run, a second one SHALL approve it, and a signed register outside the database SHALL keep each erasure and be applied again after a restore, without loosening any append-only guard.

Source: PRD-002 §20 · Priority M · Phase P1

#### Scenario: Too early, not left, held
- **WHEN** an erasure is asked for before the periods ran, for a person who has not left, or while a legal hold or an unanswered access request keeps the records
- **THEN** it is refused with its own code and nothing is created

#### Scenario: Two people
- **WHEN** an HR administrator asks and a second Owner or HR administrator approves, each with a fresh passkey
- **THEN** the worker files a signed register entry first, empties the personal columns of the records that may change, tells Fyniq with a tombstone and deletes the person's files
- **AND** the one who asked cannot approve

#### Scenario: After a restore
- **GIVEN** a restore brought back a field erased since the backup
- **WHEN** `nemura erasure-reapply` runs before the copy opens
- **THEN** every entry that verifies is applied again, and one that does not verify is reported and not applied

#### Scenario: New payslips are sealed under the person's key (rev 0.60)
- **WHEN** a payslip is made
- **THEN** its itemised figures are sealed under a key of the person's own and the erasure destroys that key, so they can never be read again while the append-only row stays

#### Scenario: What it cannot reach
- **WHEN** personal data sits in plaintext in an append-only row
- **THEN** it stays, tied to the person's opaque ID, until the owner decides how it is shredded

#### Scenario: Read by two people only (rev 0.63)
- **GIVEN** a grievance Cam filed about Sam, handled by an HR administrator
- **WHEN** Sam, another HR administrator, the Owner who is not the handler or anyone else reads it
- **THEN** they find nothing, whatever role Sam holds, and only Cam and the handler read it
- **AND** Cam never reads the inquiry record

#### Scenario: Routed away from the people it is about (rev 0.63)
- **WHEN** a grievance is about every HR administrator
- **THEN** the Owner handles it, and when it is about everyone authorised it is unrouted and waits to be taken by an authorised person who is not involved

#### Scenario: The inquiry and the outcome (rev 0.63)
- **WHEN** the handler records notes, passes it on with a reason or writes the outcome
- **THEN** each is audited without the words, entries are never changed, the outcome is written once and the person who raised it reads it

#### Scenario: A grievance whose handler has left (rev 0.64)
- **GIVEN** a grievance whose handler is deactivated or no longer holds a role that may handle one
- **WHEN** an Owner or HR administrator who is neither the filer nor anyone it is about lists grievances
- **THEN** it waits there with the reason handler_left, they may read it and take it, and the inquiry record becomes theirs to read
- **AND** the people it is about, other roles and the earlier handler still read nothing

#### Scenario: Kept five years, then deleted after review (rev 0.64)
- **GIVEN** a decided grievance five years after its outcome, with no legal hold on grievances or on anyone involved
- **WHEN** the worker's report of due deletions is reviewed by a person
- **THEN** the grievance and its inquiry record are deleted with a proof that never holds the words, and the database refuses an earlier deletion

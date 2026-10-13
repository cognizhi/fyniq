# people/fyniq-integration Specification

## Purpose
How Nemura works with Fyniq when linked: the sync, one sign-in, pay runs to the ledger, link health and the minimum data shared, with the events of Appendix E. Derived from PRD-002 §16 and Appendix E.

## Requirements

### Requirement: Events and data shared with Fyniq
Nemura and Fyniq SHALL exchange only the events, fields and scopes of PRD-002 Appendix E.

Source: PRD-002 §E

#### Scenario: Scopes
- **WHEN** Nemura calls Fyniq
- **THEN** it uses only the scopes Appendix E lists (`people.projection.write`, `payroll.journal.submit`, `payroll.payments.submit`, `people.availability.write`)

#### Scenario: Payment lines
- **WHEN** salary payment lines go to Fyniq
- **THEN** they are encrypted in transit and at rest and are not kept as contacts

### Requirement: NM-INT-01 Sync
When linked, Nemura SHALL send its events through its outbox for Fyniq to apply idempotently, departments and cost centres SHALL come from Fyniq, and people and their changes SHALL go to Fyniq's employee projection.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Departments from Fyniq
- **WHEN** the accountant renames a department in Fyniq
- **THEN** Nemura shows the new name, and the department's salary account stays Fyniq's

#### Scenario: Idempotent
- **WHEN** the same event arrives twice
- **THEN** Fyniq applies it once

### Requirement: NM-INT-02 One sign-in, one switch
A person signed in to either app SHALL be signed in to both, the Finance | People switch SHALL keep their place in each, and signing out of one SHALL sign out of both.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Sign out
- **WHEN** a person signs out of Nemura
- **THEN** they are signed out of Fyniq too

### Requirement: NM-INT-03 Pay runs to the ledger
An approved pay run SHALL be sent once with its approver, Fyniq's posting reference SHALL show on the run, and a refused posting SHALL come back with Fyniq's error code and a plain message while the run waits.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Locked period
- **WHEN** Fyniq refuses the posting because the period is locked
- **THEN** the run shows Fyniq's error code and a plain message, and waits

### Requirement: NM-INT-04 Health
The link SHALL show its state, last successful sync, waiting or failed events and a retry, and a sync failing for 24 hours SHALL be ⚠ on both apps' Checks.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Failing for a day
- **WHEN** the sync has failed for 24 hours
- **THEN** Checks shows ⚠ in Nemura (N.1 #7) and Fyniq notifies its Owner and Payroll administrator

### Requirement: NM-INT-05 Minimum data to Fyniq
Nemura SHALL send Fyniq only Appendix E's fields: never NRIC or FIN, medical, disciplinary or review data, and pay only as department totals and salary payment lines.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: No restricted data
- **WHEN** a person is synced to Fyniq
- **THEN** no NRIC or FIN, medical, disciplinary or review data is sent

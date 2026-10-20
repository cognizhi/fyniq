# automation/rules-and-schedules Specification

## Purpose
How Fyniq does work without a person starting it: the scheduler for recurring and periodic jobs, the named system principal that automated postings run under, workflow rules configured without code, and outbound webhooks for document and ledger events. Derived from PRD-001 §8.17.

## Requirements

### Requirement: FR-AUT-01 Scheduler
The system SHALL schedule recurring documents, depreciation, auto-reversals, FX revaluation, reminders, scheduled reports, statutory reminders and nightly verification, using a Cloud Scheduler tick and Cloud Tasks, so nothing has to stay running (ADR-T7).

Source: PRD-001 §8.17 · Priority M · Phase P1

#### Scenario: Scheduled work runs when due
- **WHEN** a recurring document, depreciation, an auto-reversal, an FX revaluation, a reminder, a scheduled report, a statutory reminder or the nightly verification falls due
- **THEN** the scheduler runs it

#### Scenario: Nothing has to stay running
- **WHEN** scheduled work falls due
- **THEN** a Cloud Scheduler tick starts it through Cloud Tasks
- **AND** no process has to stay running for it to run (ADR-T7)

#### Scenario: Automatic reversals run hourly (rev 0.55)
- **WHEN** the tick reaches the hourly slot of `journals.auto-reverse`
- **THEN** each posted journal due by its organisation's own today is reversed, each in its own transaction
- **AND** a run reverses at most 500 per organisation, and the next run takes the rest

### Requirement: FR-AUT-02 Automated postings under a system principal
Every automated posting SHALL run under a named system principal with its own audit identity, and SHALL respect period locks and approval policies.

Source: PRD-001 §8.17 · Priority M · Phase P1

#### Scenario: Automated posting is attributed to the system principal
- **WHEN** an automated posting is made
- **THEN** it runs under a named system principal
- **AND** the audit trail records it under that principal's own audit identity

#### Scenario: Automated posting respects period locks
- **GIVEN** a period is locked
- **WHEN** an automated posting is dated in that period
- **THEN** it is not posted in the locked period

#### Scenario: Automatic reversals under their own principal (rev 0.55)
- **WHEN** the scheduler reverses a journal by itself
- **THEN** every audit event of it names `scheduler:journals.auto-reverse`, and it runs with the worker's role, which may move a document's status and nothing else of it
- **AND** one whose period is closed or not open yet waits, with an audit event that says why, and the next run tries it again

#### Scenario: Automated posting respects approval policies
- **GIVEN** an approval policy applies to a posting
- **WHEN** that posting is made automatically
- **THEN** the approval policy applies to it

### Requirement: FR-AUT-03 Workflow rules without code
The system SHALL let users configure workflow rules ("when a bill over S$5,000 is created, notify X") without code.

Source: PRD-001 §8.17 · Priority C · Phase P2

#### Scenario: A rule is configured without code
- **WHEN** a user sets up a workflow rule
- **THEN** they configure it without writing code

#### Scenario: A rule runs when its event occurs
- **GIVEN** a workflow rule "when a bill over S$5,000 is created, notify X"
- **WHEN** a bill over S$5,000 is created
- **THEN** X is notified

### Requirement: FR-AUT-04 Outbound webhooks
The system SHALL send outbound webhooks for document and ledger events (signed, retried, logged), as specified in FR-INT-12.

Source: PRD-001 §8.17 · Priority S · Phase P2

#### Scenario: Document and ledger events are sent
- **WHEN** a document or ledger event occurs
- **THEN** an outbound webhook is sent for it as FR-INT-12 specifies

#### Scenario: Webhooks are signed
- **WHEN** an outbound webhook is sent
- **THEN** it is signed

#### Scenario: Failed deliveries are retried
- **WHEN** a webhook delivery fails
- **THEN** it is retried

#### Scenario: Deliveries are logged
- **WHEN** a webhook is delivered or a delivery fails
- **THEN** the delivery is logged

# security/detection-and-response Specification

## Purpose
How Fyniq detects and responds to security events: central, retention-locked security monitoring, alerts on risky activity, an incident response runbook tested twice a year, and cloud posture monitoring that feeds the Compliance Center. Derived from PRD-001 §13.4.

## Requirements

### Requirement: SEC-30 Security monitoring
The system SHALL send Cloud Audit Logs (admin and data access for sensitive services) and application security events to a central, retention-locked log store with alerts.

Source: PRD-001 §13.4 · Priority M · Phase P1

#### Scenario: Cloud Audit Logs collected
- **WHEN** an admin action or a data access occurs on a sensitive service
- **THEN** Cloud Audit Logs record it in the central log store

#### Scenario: Application security events collected
- **WHEN** a sign-in, an MFA change, a role change, an approval or payment event, an export or an AI exclusion hit occurs
- **THEN** an application security event is written to the central log store

#### Scenario: Log store is retention-locked
- **WHEN** an entry in the central log store is within its retention period
- **THEN** the retention lock prevents it from being deleted or changed

#### Scenario: Alerts on the log store
- **WHEN** a logged event matches an alert rule
- **THEN** an alert is raised

### Requirement: SEC-31 Security alerting
The system SHALL alert on impossible travel or new-device sign-ins for admins, vendor bank changes, mass exports, repeated authorisation failures, AI red-team pattern hits and audit-chain verification failures.

Source: PRD-001 §13.4 · Priority M · Phase P1

#### Scenario: Unusual admin sign-in
- **WHEN** an admin signs in with impossible travel or from a new device
- **THEN** an alert is raised

#### Scenario: Vendor bank change or mass export
- **WHEN** a vendor's bank details change or a mass export runs
- **THEN** an alert is raised

#### Scenario: Authorisation failures, red-team hits and audit-chain failures
- **WHEN** authorisation fails repeatedly, an AI red-team pattern is hit or an audit-chain verification fails
- **THEN** an alert is raised

### Requirement: SEC-32 Incident response runbook
The system SHALL have an incident response runbook, including the PDPA data-breach assessment and notification timelines (§14), tested by a tabletop exercise twice a year.

Source: PRD-001 §13.4 · Priority M · Phase P1

#### Scenario: Runbook covers PDPA breach timelines
- **WHEN** the incident response runbook is followed for a data breach
- **THEN** it includes the PDPA data-breach assessment and notification timelines (§14)

#### Scenario: Tabletop exercise twice a year
- **WHEN** a half-year passes
- **THEN** the runbook has been tested by a tabletop exercise in it

### Requirement: SEC-33 Cloud posture monitoring
Cloud posture SHALL be monitored (Security Command Center or equivalent), with findings feeding the Compliance Center.

Source: PRD-001 §13.4 · Priority S · Phase P1

#### Scenario: Posture findings reach the Compliance Center
- **WHEN** cloud posture monitoring (Security Command Center or equivalent) reports a finding
- **THEN** the finding appears in the Compliance Center

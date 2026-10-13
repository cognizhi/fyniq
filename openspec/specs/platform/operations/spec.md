# platform/operations Specification

## Purpose
How Fyniq is kept running and recoverable: recovery point and recovery time objectives per scenario, quarterly restore drills, retention of records, observability, and an independent heartbeat alert for the scheduler. Derived from PRD-001 §15.

## Requirements

### Requirement: NFR-DR-01 Recovery point objective
The system SHALL meet a recovery point objective per scenario: ≤ 5 minutes (point-in-time recovery) for database corruption or operator error, ≈ 0 for zone failure (DR-01), and up to 7 days for loss of the production project (DR-04).

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Database corruption or operator error
- **WHEN** data is recovered after database corruption or an operator error
- **THEN** at most 5 minutes of data is lost, by point-in-time recovery

#### Scenario: Zone failure
- **WHEN** a zone fails
- **THEN** data loss is ≈ 0 (DR-01)

#### Scenario: Loss of the production project
- **WHEN** the production project is lost
- **THEN** up to 7 days of data may be lost (the weekly export, DR-04)

#### Scenario: Loss of the region
- **WHEN** the region is lost
- **THEN** the promise is per open question 37: at P1 no copy is held outside the region

#### Scenario: G4-072 Whole region unavailable
- **GIVEN** a tabletop exercise until question 37 chooses (b) or (c), then a timed drill
- **WHEN** the whole `asia-southeast1` region is unavailable
- **THEN** the outcome matches the promise chosen in question 37, or the promise is narrowed in NFR-DR-01/02
- **AND** a database restore alone does not count as recovery

### Requirement: NFR-DR-02 Recovery time objective
The system SHALL meet a recovery time objective per scenario: ≤ 4 hours (P1) and ≤ 1 hour (P3) for database corruption or operator error, including documents, keys, sign-in and the outbox (DR-06), and automatic failover for zone failure (DR-01).

Source: PRD-001 §15 · Priority M · Phase P1 / P3

#### Scenario: Database corruption or operator error
- **WHEN** the platform is recovered after database corruption or an operator error
- **THEN** it is back in ≤ 4 hours at P1 and ≤ 1 hour at P3
- **AND** recovery includes documents, keys, sign-in and the outbox (DR-06)

#### Scenario: Zone failure
- **WHEN** a zone fails
- **THEN** the platform recovers by automatic failover (DR-01)

#### Scenario: Loss of the production project
- **WHEN** the production project is lost
- **THEN** the recovery time is measured in the drill, with no target at P1

#### Scenario: Loss of the region
- **WHEN** the region is lost
- **THEN** per open question 37, no RTO is promised at P1 and the platform returns when the region does

### Requirement: NFR-DR-03 Restore drills
A full restore to a clean environment, with ledger verification, SHALL be drilled quarterly, with its evidence stored.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Quarterly restore drill
- **WHEN** a quarter passes
- **THEN** a full restore to a clean environment has been done in it
- **AND** the ledger has been verified on the restored copy

#### Scenario: Drill evidence stored
- **WHEN** a restore drill finishes
- **THEN** its evidence is stored

### Requirement: NFR-RET-01 Retention of records
Accounting records, documents and the audit trail SHALL be retained by default for 7 years after the FY end, counted per object from the FY end, with legal hold, and kept after strike-off (§14.2).

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Default covers the statutory periods
- **WHEN** the default retention of 7 years after the FY end applies
- **THEN** it covers the Companies Act (5 years after the FY end), GST (5 years from the period end) and income tax (5 years from the Year of Assessment, 5 to 6 years after the FY end)

#### Scenario: Counted from the FY end, not from upload
- **WHEN** the retention of an object is computed
- **THEN** it is counted from the end of the FY the object belongs to, not from its upload (FR-DOC-02)

#### Scenario: Legal hold
- **WHEN** a legal hold is placed on records
- **THEN** they are kept beyond their retention until the hold is released

#### Scenario: Kept after strike-off
- **WHEN** the company is struck off
- **THEN** its records are still kept for their retention (§14.2)

### Requirement: NFR-OBS-01 Observability
Every request SHALL produce traces, metrics and structured logs with tenant and request IDs, and the platform SHALL have SLO dashboards and burn-rate alerts.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Telemetry per request
- **WHEN** a request is served
- **THEN** traces, metrics and structured logs are produced for it, carrying the tenant ID and the request ID

#### Scenario: SLO dashboards and burn-rate alerts
- **WHEN** an SLO's error budget burns faster than its alert rate
- **THEN** a burn-rate alert is raised
- **AND** the SLO is shown on its dashboard

### Requirement: NFR-OBS-02 Scheduler heartbeat
An alert independent of the API, the database and Scheduler itself SHALL notify the named platform owner within 30 minutes of the last successful tick (the Owner by escalation if it is not acknowledged), and within 2 hours for daily jobs [verify the alerting limits at P0].

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Heartbeat absence alert
- **WHEN** the heartbeat that the tick, nightly verification and anchoring write stops
- **THEN** a Cloud Monitoring metric-absence alert fires, without depending on the API, the database or Scheduler

#### Scenario: Routing and escalation
- **WHEN** the heartbeat alert fires
- **THEN** it is routed to the named platform owner, with escalation to the Owner

#### Scenario: Scheduler jobs checked daily
- **WHEN** the daily check runs
- **THEN** it confirms that the Scheduler jobs exist and are enabled

#### Scenario: G4-075 Tick disabled, or failing because the database is stopped
- **GIVEN** the Scheduler tick is disabled in staging, or separately the database is stopped so the tick fails
- **WHEN** the heartbeat stops
- **THEN** an alert reaches the platform owner through Cloud Monitoring within the NFR-OBS-02 target, without the API or the database
- **AND** after re-enabling, each schedule runs once for its latest slot and the skipped slots are recorded

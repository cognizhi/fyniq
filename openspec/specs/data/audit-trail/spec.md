# data/audit-trail Specification

## Purpose
How Fyniq records every saved change so that the record cannot be altered or removed without detection: the five audit layers (immutable ledger, business audit events, anchoring, database and platform logs, analytical copy), the audit event schema, sensitive values, integrity verification, period-close snapshots, read and export auditing, audit retention, the audit explorer, and purge and restore that keep the chain verifiable. Derived from PRD-001 §11.5.

## Requirements

### Requirement: Every saved change is recorded and tamper-evident
Every saved change, by any human, AI-originated proposal, integration, scheduler or administrator, SHALL be recorded with who, what (before and after), when, where from, why, and under which approval, and the record SHALL NOT be alterable or removable without detection (AUD-00).

Source: PRD-001 §11.5

#### Scenario: Any actor's change is recorded
- **WHEN** a human, an AI-originated proposal, an integration, the scheduler or an administrator saves a change
- **THEN** the change is recorded with who, what (before and after), when, where from, why, and under which approval

#### Scenario: Tampering is detected
- **WHEN** a recorded change is altered or removed outside the audited paths
- **THEN** the alteration or removal is detected

### Requirement: Immutable ledger layer
The system SHALL keep every posting in an insert-only ledger for at least the retention period (§11.9), correcting postings only by reversal.

Source: PRD-001 §11.5

#### Scenario: Postings cannot be updated or deleted
- **WHEN** any application role tries to update or delete a posting
- **THEN** it has no UPDATE or DELETE privilege on the journal entries and lines
- **AND** a trigger blocks the change

#### Scenario: Corrections by reversal
- **WHEN** a posting must be corrected
- **THEN** it is corrected by a reversal

#### Scenario: Purging postings is an open question
- **WHEN** postings pass their retention period
- **THEN** whether they are ever purged is decided under open question 38

### Requirement: Business audit events layer
The system SHALL record a business audit event in the same transaction as every create, update, void, approval, rejection, login, permission change, export, AI proposal and execution.

Source: PRD-001 §11.5

#### Scenario: What a business audit event records
- **WHEN** a business audit event is written
- **THEN** it records the actor (user, AI or service), the on-behalf-of user, the approval ID, the request ID, the IP or device, the reason and the before/after field diff

#### Scenario: Application event with a trigger backstop
- **WHEN** a change is saved
- **THEN** the application emits a typed event in the same transaction
- **AND** a database trigger captures the row-level before/after JSON as a backstop for any write path the application missed

#### Scenario: Per-organisation hash chain
- **WHEN** an event is appended to the insert-only audit table
- **THEN** it stores `seq`, `prev_hash` and `hash = SHA-256(prev_hash ‖ canonical_event_json)` (pgcrypto)
- **AND** appends are serialised per organisation with a transaction-scoped advisory lock

### Requirement: Anchoring layer
The system SHALL anchor each organisation's new audit events and ledger entries hourly and at every period close, as a signed checkpoint of the chain head or Merkle root.

Source: PRD-001 §11.5

#### Scenario: Anchors are signed
- **WHEN** a checkpoint is taken
- **THEN** it is signed with a Cloud KMS asymmetric key (e.g. EC P-256)
- **AND** the public key allows offline verification and the signature proves origin

#### Scenario: Anchors are retention-locked in Singapore
- **WHEN** a checkpoint is stored
- **THEN** it is written to a Cloud Storage bucket in Singapore with Bucket Lock (retention up to 100 years; irreversible) or Object Retention Lock in locked mode

#### Scenario: Nobody can alter an anchor
- **WHEN** anyone, including a platform administrator, tries to alter or delete an anchor before its retention ends
- **THEN** the anchor cannot be altered or deleted

#### Scenario: Optional external timestamp
- **WHEN** an external time-stamping authority is configured
- **THEN** the checkpoint also gets an RFC 3161 timestamp

### Requirement: Database and platform logs layer
The system SHALL record SQL statements by privileged sessions, schema changes, and the admin and data-access logs of cloud services, in retention-locked log storage in a separate project with restricted access.

Source: PRD-001 §11.5

#### Scenario: Database statements
- **WHEN** a privileged session runs SQL or changes the schema
- **THEN** pgAudit on the database records it

#### Scenario: Cloud audit logs
- **WHEN** cloud services are used
- **THEN** Cloud Audit Logs record Admin Activity, always on, in the 400-day `_Required` bucket
- **AND** Data Access logs, off by default, are enabled for Cloud SQL, Cloud KMS and Cloud Storage

#### Scenario: Locked log bucket
- **WHEN** logs are routed
- **THEN** log sinks route them to a locked log bucket in `asia-southeast1` (retention up to 3,650 days)
- **AND** that bucket is in a separate project with restricted access

### Requirement: Analytical copy layer
When BigQuery is introduced (§11.7), the system SHALL keep a queryable copy of the ledger and audit events, fed by change-data-capture, for investigations and analytics.

Source: PRD-001 §11.5

#### Scenario: Independent copy shows discrepancies
- **GIVEN** the analytical copy is in place
- **WHEN** the ledger or audit events in the database differ from the copy
- **THEN** the discrepancy is detectable

### Requirement: AUD-01 Audit event schema
Audit events SHALL use a versioned, documented schema.

Source: PRD-001 §11.5 · Priority M · Phase P0

#### Scenario: Schema fields
- **WHEN** an audit event is written
- **THEN** it has `event_id`, `organisation_id`, `occurred_at`, `actor` {type, id}, `on_behalf_of`, `originator` (AI proposal ID), `approval_ids[]`, `action`, `object` {type, id, version}, `changes` {field: [before, after]}, `reason`, `request` {id, ip, user-agent, session}, `prev_hash` and `hash`

#### Scenario: Schema is versioned and documented
- **WHEN** the audit event schema changes
- **THEN** it gets a new version
- **AND** the change is documented

### Requirement: AUD-02 Sensitive values in the trail
Sensitive values in `changes` (NRIC, bank numbers, salaries) SHALL be stored encrypted or as keyed hashes, so the trail proves change without exposing the value to every auditor.

Source: PRD-001 §11.5 · Priority M · Phase P1

#### Scenario: Change proven without the value
- **GIVEN** an employee's bank number or salary changed
- **WHEN** an auditor reads the audit event
- **THEN** the event proves the value changed
- **AND** the value itself is stored encrypted or as a keyed hash, not in clear

### Requirement: AUD-03 Verify integrity
Anyone with audit access SHALL be able to run "Verify integrity" for a date range, which recomputes the chain and checks it against the signed anchors.

Source: PRD-001 §11.5 · Priority M · Phase P0 (nightly verification and anchoring in staging) / P1 (Verify integrity for users)

#### Scenario: User runs Verify integrity
- **GIVEN** a user with audit access
- **WHEN** they run "Verify integrity" for a date range
- **THEN** the platform recomputes the chain for that range and checks it against the signed anchors

#### Scenario: Nightly verification
- **WHEN** the nightly job runs
- **THEN** it verifies the chain against the anchors for all organisations (INV-12)

#### Scenario: G4-071 Purge, then a restore behind the last anchor
- **WHEN** expired audit segments are purged (if question 38 allows), then a point-in-time restore lands behind the last anchor
- **THEN** AUD-03 verifies the remaining chain offline from the boundary record
- **AND** the missing anchored range is reported as an explained recovery event, not as clean or as tampering

### Requirement: AUD-04 Period-close snapshots
At period close the system SHALL hash, sign and anchor the trial balance, the subledger totals and the chain head.

Source: PRD-001 §11.5 · Priority M · Phase P1

#### Scenario: Snapshot at close
- **WHEN** a period is closed
- **THEN** the trial balance, subledger totals and chain head are hashed, signed and anchored

#### Scenario: Reopening records both snapshots
- **WHEN** a closed period is reopened
- **THEN** the before and after snapshots are recorded

#### Scenario: Snapshots under the close locks
- **WHEN** a snapshot and the period's status change are taken
- **THEN** they are taken under the ENG-47 locks
- **AND** they record their ledger revision

### Requirement: AUD-05 Sensitive reads and exports are audited
Read access to sensitive data (payroll, personal identifiers) and every export SHALL be audited.

Source: PRD-001 §11.5 · Priority M · Phase P1

#### Scenario: Reading sensitive data
- **WHEN** a user reads payroll data or personal identifiers
- **THEN** the read is audited

#### Scenario: Exporting
- **WHEN** any export is made
- **THEN** the export is audited

### Requirement: AUD-06 Audit data retention
Audit events about accounting records SHALL be retained at least as long as those records (§14), and security events for the security-log retention; audit data SHALL be excluded from user deletion and from PDPA erasure, retained under the legal-obligation basis with access restricted.

Source: PRD-001 §11.5 · Priority M · Phase P1

#### Scenario: Deletion and erasure requests leave the trail
- **WHEN** a user deletion or a PDPA erasure request is carried out
- **THEN** the audit events are kept, with access restricted

#### Scenario: Events about records follow the records
- **WHEN** retention is set for audit events about accounting records, documents, approvals and payments
- **THEN** they are retained at least as long as those records (§11.9), because the law requires the records (PDPA s25 allows retention for legal purposes)

#### Scenario: Security events follow the security-log retention
- **WHEN** retention is set for sign-in, device and read-access events (AUD-05)
- **THEN** they follow the security-log retention, stated with its reason in the data inventory (CMP-PD-02)

#### Scenario: Restricted access does not end retention
- **WHEN** access to audit events is restricted
- **THEN** their retention continues

#### Scenario: At expiry
- **WHEN** the retention of audit events ends
- **THEN** their personal values are shredded (§11.8) or the events are purged under AUD-09

### Requirement: AUD-07 Audit explorer and export
The system SHALL provide an audit explorer and export (FR-CTL-03), and the auditor evidence pack SHALL include the chain verification results.

Source: PRD-001 §11.5 · Priority M · Phase P1 / P2

#### Scenario: Explore and export
- **WHEN** a user with audit access opens the audit explorer
- **THEN** they can browse and export audit events (FR-CTL-03)

#### Scenario: Auditor pack includes verification
- **WHEN** an auditor evidence pack is produced
- **THEN** it includes the chain verification results

### Requirement: AUD-08 The audit trail cannot be switched off
The audit trail SHALL NOT be switched off or reduced by any setting, role or plan.

Source: PRD-001 §11.5 · Priority M · Phase P0

#### Scenario: No setting, role or plan reduces it
- **WHEN** any setting, role or plan is applied
- **THEN** the audit trail is neither switched off nor reduced

#### Scenario: The only removal is the audited purge
- **WHEN** audit events are removed
- **THEN** it is only through the audited retention purge of AUD-09

### Requirement: AUD-09 Purge and restore keep the chain verifiable
Purging expired audit data and restoring the database SHALL keep the audit chain verifiable.

Source: PRD-001 §11.5 · Priority M · Phase P1

#### Scenario: Purge removes whole expired segments
- **WHEN** audit data is purged
- **THEN** only whole, expired chain segments are removed
- **AND** a signed, anchored boundary record is written first, with the organisation, the last purged `seq` and `hash`, the purge approval and counts

#### Scenario: Verification starts from the boundary record
- **GIVEN** a purge has written a boundary record
- **WHEN** verification (AUD-03) runs
- **THEN** it starts from the latest boundary record

#### Scenario: Restore behind published anchors
- **WHEN** a point-in-time restore lands behind published anchors
- **THEN** the platform keeps the anchors
- **AND** it records a signed recovery event naming the restore point and the anchored range now missing
- **AND** it raises an incident (SEC-32) and continues the chain from the restored head

#### Scenario: The gap is explained, never clean
- **GIVEN** a recovery event records a missing anchored range
- **WHEN** verification runs over that range
- **THEN** it reports the gap as explained, never as clean

#### Scenario: G4-070 Shredded cohort and a restore from an older backup
- **WHEN** one cohort of an employee's data expires and is shredded, and a monthly backup from before the shredding is restored into the drill project
- **THEN** the shredded values stay unreadable after the restore
- **AND** all ledger amounts stay readable
- **AND** the anonymisation register is re-applied before the copy opens

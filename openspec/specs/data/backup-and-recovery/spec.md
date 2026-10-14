# data/backup-and-recovery Specification

## Purpose
How Fyniq keeps its data recoverable and for how long: database high availability, point-in-time recovery and backups pinned to Singapore, the disaster-recovery posture, restore drills, attachment storage, and the retention and deletion of each class of data. Derived from PRD-001 §11.8 and PRD-001 §11.9.

## Requirements

### Requirement: DR-01 Regional high availability
The production database SHALL run with regional HA, with a synchronous standby in a second zone.

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: Zone loss
- **WHEN** the production database's zone is lost
- **THEN** the synchronous standby in the second zone serves the database
- **AND** the recovery point is ≈ 0 (RPO ≈ 0 for zone loss)

### Requirement: DR-02 Point-in-time recovery and backups
The database SHALL have point-in-time recovery plus daily automated backups retained 35 days and monthly backups retained 1 year.

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: Point-in-time recovery window
- **WHEN** the database is restored to a point in time
- **THEN** any point in the last 7 days is available on Enterprise, and in the last 35 days on Enterprise Plus

#### Scenario: Daily and monthly backups
- **WHEN** backups are taken
- **THEN** a daily automated backup is kept 35 days
- **AND** a monthly backup is kept 1 year

### Requirement: DR-03 Backups pinned to Singapore
Backups SHALL be pinned to `asia-southeast1` by the organisation policy `gcp.resourceLocations`.

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: Backup location
- **WHEN** a backup is created
- **THEN** it is stored in `asia-southeast1`
- **AND** the organisation policy `gcp.resourceLocations` prevents any other location

#### Scenario: Singapore data boundary for P3
- **WHEN** P3 is planned
- **THEN** the Assured Workloads Singapore Data Boundary package (free tier) is evaluated for fit against the products used

### Requirement: DR-04 Disaster-recovery copy and posture
A disaster-recovery copy outside Singapore SHALL be made only after a documented PDPA transfer-limitation assessment (§14).

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: No copy abroad without an assessment
- **GIVEN** every other Google Cloud region is abroad (Jakarta, Bangkok, Kuala Lumpur, Taiwan, Hong Kong)
- **WHEN** a disaster-recovery copy outside Singapore is proposed
- **THEN** it is made only after a documented PDPA transfer-limitation assessment

#### Scenario: Posture until then
- **GIVEN** no transfer assessment has been documented
- **WHEN** the disaster-recovery posture is described
- **THEN** it is the weekly encrypted export in a retention-locked Singapore bucket plus regional HA

#### Scenario: The export survives loss of the production project
- **WHEN** the weekly export is written
- **THEN** its bucket is in the `fyniq-audit` project (§12.4)
- **AND** it survives loss of `fyniq-prod`

#### Scenario: What the posture covers
- **WHEN** zone failure, corruption, operator error or loss of the production project occurs
- **THEN** the posture covers it
- **AND** it does not cover loss of the region, because every copy and key is in `asia-southeast1` (open question 37)

### Requirement: DR-05 Enhanced backups in a vault
The system SHALL keep enhanced backups in a Backup and DR vault in a separate project, with enforced retention and retention lock.

Source: PRD-001 §11.8 · Priority S · Phase P3 (Stage 2)

#### Scenario: Vault backups cannot be removed early
- **WHEN** an enhanced backup is in the vault
- **THEN** it is in a project separate from production
- **AND** its retention is enforced and locked

### Requirement: DR-06 Quarterly restore drill
The system SHALL run a quarterly restore drill into an isolated project, and ledger verification and audit-chain verification SHALL pass on the restored copy (NFR-DR-03).

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: Verification on the restored copy
- **WHEN** the quarterly drill restores into an isolated project
- **THEN** ledger verification and audit-chain verification pass on the restored copy

#### Scenario: Each drill names and times its scenario
- **WHEN** a drill runs
- **THEN** it names its scenario (NFR-DR-01/02)
- **AND** it is timed against that scenario's targets

#### Scenario: What counts as recovered
- **WHEN** a drill's result is recorded
- **THEN** it counts as recovered only when documents, keys, sign-in, the Scheduler tick and outbox reconciliation (ADR-T7) work too

#### Scenario: G4-074 Restore before outbox rows already executed
- **WHEN** the database is restored to a point before outbox rows that were already executed more than 24 hours earlier
- **THEN** those rows are marked unknown-outcome and reconciled
- **AND** there is no duplicate posting, email or provider call
- **AND** a Cloud Run job started before the restore is linked to its row

#### Scenario: With Nemura linked
- **WHEN** a drill runs while Nemura is linked
- **THEN** it also restores one app alone and checks the cross-app reconciliation (FR-PPL-19)

### Requirement: DR-07 Attachment storage
Attachments SHALL be kept in a regional Cloud Storage bucket in Singapore with object versioning and a retention policy matching the retention table (§11.9).

Source: PRD-001 §11.8 · Priority M · Phase P1

#### Scenario: Per-object lock over a backstop policy
- **WHEN** an attachment is committed
- **THEN** it has a per-object retain-until in locked mode (FR-DOC-02)
- **AND** the bucket policy is a backstop beneath it

#### Scenario: Retention Lock enabled at creation
- **GIVEN** Google allows enabling Object Retention Lock on an existing bucket only in the console
- **WHEN** the attachment bucket is created through IaC
- **THEN** Object Retention Lock is enabled at creation

#### Scenario: Separate staging bucket
- **WHEN** a file is uploaded
- **THEN** it goes to a separate, unlocked staging bucket (FR-DOC-13)

### Requirement: Retention of the ledger, documents, attachments and audit trail
The ledger, documents, attachments and audit trail SHALL be retained for the statutory minimum (§14) after the end of the financial year they relate to, 7 years by default, and SHALL then be removed by an automatic, audited purge job.

Source: PRD-001 §11.9

#### Scenario: Which financial year counts
- **WHEN** the retention of a record is computed
- **THEN** it runs from the end of the financial year in which the transactions it records are completed, Companies Act s199(1) and (2A)

#### Scenario: Default and legal hold
- **WHEN** no other retention is set
- **THEN** the retention is 7 years
- **AND** a legal hold extends it

#### Scenario: Files use their retain-until
- **WHEN** the retention date of a file is needed
- **THEN** it is the object's retain-until (FR-DOC-02)

#### Scenario: Purge after expiry
- **WHEN** the retention expires
- **THEN** an automatic purge job removes the data
- **AND** the purge is itself audited
- **AND** anchors are kept until their own retention ends

### Requirement: Retention of personal data not needed for records
Personal data of employees and contacts that is not needed for the records SHALL be kept only as long as needed for its purpose.

Source: PRD-001 §11.9

#### Scenario: PDPA requests
- **WHEN** a PDPA request is honoured
- **THEN** fields not required for statutory records are anonymised
- **AND** statutory records are retained under the legal-obligation exception, with access restricted

#### Scenario: Withdrawal of consent
- **WHEN** a person withdraws consent
- **THEN** further use of their data stops

#### Scenario: Purpose ends
- **WHEN** the purpose of personal data ends
- **THEN** fields not required for statutory records are anonymised

#### Scenario: Anonymisations survive a restore
- **WHEN** a field is anonymised
- **THEN** the anonymisation is entered in the register of §11.8
- **AND** a restore re-applies it

### Requirement: Retention of AI conversations
AI conversations SHALL be kept 1 year by default (configurable) and then removed by a purge job.

Source: PRD-001 §11.9

#### Scenario: Conversations are purged
- **WHEN** an AI conversation is older than the configured retention (1 year by default)
- **THEN** the purge job removes it

#### Scenario: Proposal-related activity follows audit retention
- **WHEN** an AI activity log entry relates to a proposal
- **THEN** it follows the audit retention, not the conversation retention

### Requirement: Retention of application logs
Application logs that are not audit logs SHALL be kept 30–90 days and deleted automatically.

Source: PRD-001 §11.9

#### Scenario: Application logs expire
- **WHEN** a non-audit application log entry reaches the end of its 30–90 day retention
- **THEN** it is deleted automatically

### Requirement: Retention of backups
Backups SHALL be kept with point-in-time recovery for 7 days (35 on Enterprise Plus), daily backups for 35 days and monthly snapshots for 1 year, and deleted automatically.

Source: PRD-001 §11.9

#### Scenario: Backups expire
- **WHEN** a point-in-time log, daily backup or monthly snapshot passes its retention (7 days, or 35 on Enterprise Plus; 35 days; 1 year)
- **THEN** it is deleted automatically

### Requirement: Personal data in immutable records is crypto-shredded at the end of retention
Personal data inside immutable records (payroll, audit `changes`) SHALL still be removed when its retention period ends (PDPA s25), by destroying the encryption key of its person and retention cohort, without breaking the hash chain. Confirm the approach with counsel (open question 39).

Source: PRD-001 §11.8

#### Scenario: Keys per person and retention cohort
- **WHEN** a sensitive personal value is stored in an immutable record
- **THEN** it is encrypted with a data key per person and retention cohort (for example per person per financial year)
- **AND** amounts the books need are never under a shreddable key

#### Scenario: One cohort shredded, later records readable
- **GIVEN** the retention obligation for one cohort of a person has ended
- **WHEN** that cohort's key is destroyed
- **THEN** that cohort's data is unreadable
- **AND** the person's later records stay readable

#### Scenario: Hash chain unaffected
- **GIVEN** the hash chain covers the ciphertext
- **WHEN** a cohort is crypto-shredded
- **THEN** the chain still verifies

#### Scenario: Destruction recorded with proof
- **WHEN** a key is destroyed
- **THEN** the destruction is recorded with the key ID, the time and the approver

#### Scenario: No erasure on request alone
- **WHEN** a person asks for their data to be erased while its retention obligation continues
- **THEN** the data is not shredded on that basis, because the PDPA gives no general right to erasure on request

### Requirement: Shredded keys do not survive in backups or exports
Wrapped keys SHALL NOT outlive their destruction in backups or the weekly export: either the key-encryption key in Cloud KMS is per cohort and its versions are destroyed, or a restore re-applies a shredding and anonymisation register, kept outside the database backup, before the restored copy is opened.

Source: PRD-001 §11.8

#### Scenario: Restore re-applies the shredding register
- **GIVEN** the shredding and anonymisation register approach is used
- **WHEN** a backup or weekly export is restored
- **THEN** every shredding and anonymisation in the register is re-applied before the restored copy is opened

#### Scenario: The register lives outside the database backup
- **WHEN** the shredding and anonymisation register is stored
- **THEN** it is kept outside the database backup

#### Scenario: Per-cohort key-encryption keys
- **GIVEN** the per-cohort key-encryption-key approach is used
- **WHEN** a cohort is shredded
- **THEN** the versions of its key-encryption key in Cloud KMS are destroyed

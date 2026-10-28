# security/data-protection Specification

## Purpose
How Fyniq protects the data it holds: encryption in transit, at rest and per sensitive field, secrets, data residency, personal-data minimisation, log redaction, and keeping local adapters out of staging and production. Derived from PRD-001 §13.2.

## Requirements

### Requirement: SEC-10 Encryption in transit and at rest
The system SHALL encrypt data in transit (TLS 1.2+, HSTS) and at rest (Google-managed keys by default; customer-managed keys in Cloud KMS for the database, document storage and backups).

Source: PRD-001 §13.2 · Priority M · Phase P1

#### Scenario: Encryption in transit
- **WHEN** a client connects to Fyniq
- **THEN** the connection uses TLS 1.2 or later
- **AND** HSTS is set

#### Scenario: Encryption at rest by default
- **WHEN** data is stored
- **THEN** it is encrypted at rest with Google-managed keys by default

#### Scenario: Customer-managed keys for the database, documents and backups
- **WHEN** the database, document storage or backups are stored
- **THEN** they are encrypted with customer-managed keys in Cloud KMS

### Requirement: SEC-11 Application-level encryption of sensitive fields
The system SHALL encrypt the most sensitive fields (NRIC/FIN, bank account numbers, salary details) at application level with envelope encryption in Cloud KMS, and SHALL mask them by default in the UI, logs and AI context.

Source: PRD-001 §13.2 · Priority M · Phase P1

#### Scenario: Envelope encryption of sensitive fields
- **WHEN** an NRIC/FIN, a bank account number or a salary detail is stored
- **THEN** it is encrypted at application level with envelope encryption using Cloud KMS

#### Scenario: Masked by default
- **WHEN** one of these fields is shown in the UI, written to a log or placed in AI context
- **THEN** it is masked by default

#### Scenario: The data key is fresh and the record bound (rev 0.152)
- **WHEN** a field is sealed
- **THEN** it is encrypted with a fresh AES-256-GCM data key wrapped by the Cloud KMS key, bound to its record so a copy in another record does not open
- **AND** a value sealed before the wrapping key rotated still opens

#### Scenario: Fingerprints without the value (rev 0.152)
- **WHEN** equal sealed values must be found, or a change proved in the audit trail
- **THEN** a keyed HMAC-SHA256 fingerprint within the organisation is compared, under a pinned key version, and the value is never decrypted for it

### Requirement: SEC-12 Secrets in Secret Manager only
Secrets SHALL be kept only in Secret Manager, SHALL be rotated, and SHALL never be in code, images or logs.

Source: PRD-001 §13.2 · Priority M · Phase P0

#### Scenario: Services read secrets from Secret Manager
- **WHEN** a service needs a secret
- **THEN** it reads the secret from Secret Manager

#### Scenario: Secrets are rotated
- **WHEN** a secret is kept in Secret Manager
- **THEN** it is rotated

#### Scenario: Secret scanning in CI
- **WHEN** CI runs
- **THEN** secret scanning checks that no secret is in the code or the images

### Requirement: SEC-13 Data residency in Singapore
Primary storage, backups and the disaster-recovery export SHALL stay in Singapore (`asia-southeast1`) (DR-03, DR-04); the second region of earlier revisions is superseded.

Source: PRD-001 §13.2 · Priority M · Phase P1

#### Scenario: Primary storage in Singapore
- **WHEN** data is written to primary storage
- **THEN** it is stored in `asia-southeast1`

#### Scenario: Backups and disaster-recovery export
- **WHEN** a backup or the disaster-recovery export is written
- **THEN** it stays in `asia-southeast1` (DR-03, DR-04)

#### Scenario: A copy outside Singapore
- **WHEN** a copy outside Singapore is proposed
- **THEN** it needs the PDPA transfer assessment and the decision on open question 37, which keeps P1 Singapore-only

#### Scenario: Cross-border flows are listed
- **WHEN** data flows across the border, including an AI model call
- **THEN** the flow is listed in the data inventory

### Requirement: SEC-14 Personal data minimisation
The system SHALL minimise personal data: it SHALL collect NRIC/FIN only where the law requires it (payroll, IR8A) and SHALL hold no card data.

Source: PRD-001 §13.2 · Priority M · Phase P1

#### Scenario: NRIC/FIN only where the law requires it
- **WHEN** personal data is collected
- **THEN** NRIC/FIN is collected only where the law requires it (payroll, IR8A)

#### Scenario: No card data
- **WHEN** a card payment is taken
- **THEN** it goes through a provider's hosted payment pages
- **AND** Fyniq holds no card data, so PCI scope is avoided

### Requirement: SEC-15 Logs free of secrets and identifiers
Logs SHALL never contain secrets, tokens, full NRIC or bank numbers.

Source: PRD-001 §13.2 · Priority M · Phase P0

#### Scenario: Redaction before logging
- **WHEN** a log entry would include a secret, a token, a full NRIC or a bank number
- **THEN** the value is redacted before the entry is written

#### Scenario: Log redaction is tested
- **WHEN** the test suite runs
- **THEN** log redaction is tested

### Requirement: SEC-16 Local adapters never reach staging or production
Local adapters (software keys, emulators, replayed AI) SHALL never reach staging or production (DEV-07).

Source: PRD-001 §13.2 · Priority M · Phase P0

#### Scenario: Production refuses to start with a local adapter
- **GIVEN** a local adapter is configured
- **WHEN** a production service starts
- **THEN** it refuses to start

#### Scenario: Staging and production run without local adapters
- **WHEN** a build runs in staging or production
- **THEN** it uses no software keys, emulators or replayed AI (DEV-07)

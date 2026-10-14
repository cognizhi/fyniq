# people/platform Specification

## Purpose
Nemura's shape: a standalone people platform with its own image, database and API, linkable to Fyniq, sharing Fyniq's rules for money, audit and correctness, and its non-functional targets. Derived from PRD-002 §4 and §21.

## Requirements

### Requirement: Architecture decisions for Nemura
Nemura SHALL follow the decisions of PRD-002 §4: its own image and database in the shared monorepo (ADR-N1), PRD-001 §8.7 as its payroll rules (ADR-N2), Fyniq's identity provider (ADR-N3), one owner per fact (ADR-N4), events and the public API between the apps (ADR-N5) and its own design system (ADR-N6).

Source: PRD-002 §4

#### Scenario: One owner per fact
- **WHEN** people data and the books both need a fact
- **THEN** Nemura owns people data and Fyniq owns the books, and neither writes the other's tables

#### Scenario: No shared database
- **WHEN** Nemura and Fyniq exchange data
- **THEN** they use outbox events and Fyniq's public API, never a shared database or a private endpoint

#### Scenario: Payroll formulas
- **WHEN** Nemura calculates pay
- **THEN** it uses PRD-001 Appendix B.3's formulas (ADR-N2; Appendix F is PRD-001's control registry)

#### Scenario: One identity, two sessions
- **WHEN** a person signs in
- **THEN** each app keeps its own session made from the shared sign-in, and signing out revokes both (ADR-N3, NM-INT-12)

#### Scenario: Event envelope
- **WHEN** events pass between the apps
- **THEN** each carries the envelope of NM-INT-10 (ADR-N5)

### Requirement: NM-ARC-01 Standalone first
Nemura SHALL deploy, sign people in and run every feature with no Fyniq installation, producing a payroll journal file and a bank payment file when it is not linked.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: No Fyniq
- **GIVEN** an installation with no Fyniq
- **WHEN** a company runs leave, payroll and self-service
- **THEN** every feature works

#### Scenario: Files instead of postings
- **GIVEN** Nemura is not linked to Fyniq
- **WHEN** a pay run is approved
- **THEN** it produces a generic journal CSV and a bank payment file in a supported bank format, each with a manifest
- **AND** for a bank not on the supported list, the CSV and a manual payment

### Requirement: NM-ARC-02 One image, own database
Nemura SHALL ship as one lean, hardened image with its own PostgreSQL database and migrations, and SHALL run locally with the same stack and substitutes as Fyniq.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: Lean image
- **WHEN** the `nemura` image is built
- **THEN** it is a static Go binary with the web app on `distroless/static:nonroot`, non-root, read-only, pinned by digest and within the size budget (PRD-001 §12.9)

#### Scenario: Own database
- **WHEN** Nemura stores data
- **THEN** it uses its own PostgreSQL database and migrations, never Fyniq's

### Requirement: NM-ARC-03 API-first
Every Nemura capability SHALL be an operation in `contracts/openapi/nemura/` before it is a screen, following Fyniq's API conventions.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: Conventions
- **WHEN** a Nemura operation is added
- **THEN** it uses RFC 9457 errors with stable codes, `Idempotency-Key` on writes, `ETag`/`If-Match` on versioned records and additive changes inside `/v1`

### Requirement: NM-ARC-04 Linking to Fyniq
Linking a Nemura organisation to a Fyniq organisation SHALL need an Owner of both and a second approver, register Nemura as an integration client with the scopes of Appendix E only, and start the sync; unlinking SHALL keep both sides' data.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: Link
- **WHEN** an Owner of both links the organisations and a second approver approves
- **THEN** Nemura is registered in Fyniq as an integration client with only the Appendix E scopes, and the sync starts

#### Scenario: Unlink
- **WHEN** the link is removed
- **THEN** the sync stops and neither side's data is deleted

#### Scenario: Linking an organisation that ran standalone
- **GIVEN** a Nemura organisation on its own identity tenant
- **WHEN** it is linked to Fyniq
- **THEN** each person is mapped explicitly, never by email alone (NM-INT-12)

#### Scenario: Unlinking settles first
- **WHEN** the Owner starts to unlink
- **THEN** work in flight is settled before the link's credentials are revoked (NM-INT-11)

### Requirement: NM-ARC-05 Shared rules
Nemura SHALL follow Fyniq's rules for exact-decimal money, auditing in the same transaction, correcting a run by posting the difference of a new version and reversing a posting that should not exist, and payroll checked against an independent oracle.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: Money and audit
- **WHEN** Nemura saves a change or calculates pay
- **THEN** amounts are exact decimals with named rounding policies, and the change is audited in the same transaction (AUD-01)

#### Scenario: Oracle
- **WHEN** payroll is calculated
- **THEN** the Go engine's result equals the Python oracle's to the cent

#### Scenario: Corrections
- **WHEN** a run is corrected
- **THEN** a new version posts the difference from what was posted, as PRD-001 B.3 does (NM-PAY-09)

### Requirement: NM-ARC-06 Service identities and the standalone profile
Nemura's services SHALL each run with their own identity and database role, the people-assistant runtime SHALL hold no pay-run, letter-issue or record-write role, and the standalone profile SHALL run sign-in, pay runs, documents and file exports with no Fyniq service.

Source: PRD-002 §4 · Priority M · Phase P1

#### Scenario: Standalone isolation
- **GIVEN** every Fyniq service is stopped
- **WHEN** a company signs in, calculates and approves a run, issues payslips and KETs, and exports the journal and bank files
- **THEN** all of it works

#### Scenario: The assistant cannot write
- **WHEN** the people-assistant runtime tries to write a pay run, a record or an issued letter
- **THEN** its database role refuses it, and it can only insert a proposal

### Requirement: NM-NFR-01 Region and residency
Nemura SHALL run in Google Cloud `asia-southeast1`, work offline in local mode once it has code, and scale to zero.

Source: PRD-002 §21 · Priority M · Phase P1

#### Scenario: Region
- **WHEN** Nemura is deployed to the cloud
- **THEN** it runs in `asia-southeast1`

### Requirement: NM-NFR-02 Performance
My home and My pay SHALL render within 1.5 s at the 75th percentile on a mid-range phone on 4G, and a 50-person pay run SHALL calculate within 5 s.

Source: PRD-002 §21 · Priority S · Phase P1

#### Scenario: Self-service speed
- **WHEN** an employee opens My home or My pay on a mid-range phone on 4G
- **THEN** the page renders within 1.5 s at the 75th percentile

#### Scenario: Pay run speed
- **WHEN** a pay run for 50 people is calculated
- **THEN** it completes within 5 s

### Requirement: NM-NFR-03 Availability
Self-service SHALL be available 99.5% of a month, pay-day jobs SHALL retry and alert, and Nemura SHALL keep working and queue the hand-off when Fyniq is down.

Source: PRD-002 §21 · Priority M · Phase P1

#### Scenario: Fyniq down
- **GIVEN** Fyniq is unavailable
- **WHEN** a pay run is approved
- **THEN** Nemura keeps working and the hand-off waits in its outbox until Fyniq answers

#### Scenario: Pay-day path
- **WHEN** a pay-day job fails
- **THEN** NM-NFR-05's alerting applies

### Requirement: NM-NFR-04 Localisation
Nemura SHALL use English first and the worksite pack's date, number and currency formats, with templates and packs ready for other languages.

Source: PRD-002 §21 · Priority S · Phase P1

#### Scenario: Singapore formats
- **WHEN** a Singapore worksite's dates and amounts are shown
- **THEN** they use `en-SG` formats and S$

### Requirement: NM-NFR-05 Pay-day path
Between a run's input cut-off and its pay day, the approval, hand-off and payment-file jobs SHALL alert the Payroll administrator and the Owner within 1 hour of a failure.

Source: PRD-002 §21 · Priority M · Phase P1

#### Scenario: Hand-off fails before pay day
- **GIVEN** the October run was approved on Fri 23 Oct for pay day Wed 28 Oct
- **WHEN** the hand-off fails on Mon 26 Oct
- **THEN** the Payroll administrator and the Owner are alerted within 1 hour

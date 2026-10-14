# data/data-model Specification

## Purpose
The logical data model beneath every Fyniq feature and the rules every table follows: keys, tenancy and row-level security, global tables, versioning, the insert-only ledger, money columns, effective-dated reference data, idempotency, the single posting path and balances, and the difference between accounting date and recorded time. Derived from PRD-001 §11.4 and §11.6.

## Requirements

### Requirement: Logical data model
The system SHALL hold each organisation's data under that organisation, in the entities of the logical data model of §11.4.

Source: PRD-001 §11.4

#### Scenario: Periods, accounts, dimensions and parameter packs
- **WHEN** an organisation (and, from P3, each of its entities) is set up
- **THEN** it holds periods whose status is open, soft-locked or closed, accounts with their versions, dimensions with their values, and effective-dated parameter pack versions (GST, CPF, …)

#### Scenario: Memberships and master data
- **WHEN** users, contacts, items and employees are added to an organisation
- **THEN** each user membership carries role assignments with a scope and an expiry
- **AND** employees' identifiers are encrypted

#### Scenario: Documents keep immutable versions
- **WHEN** a document (invoice, bill, bank transaction, pay run, asset event, journal or claim) changes
- **THEN** a new immutable version snapshot is kept
- **AND** the document points to its current version

#### Scenario: Append-only and immutable records
- **WHEN** journal entries and lines, equity transactions, agent run reports, knowledge-base page versions or audit events are written
- **THEN** they are append-only
- **AND** audit events are hash-chained per organisation, and template versions are immutable with a hash

#### Scenario: Balances are materialised and rebuildable
- **WHEN** balances are kept
- **THEN** they are held per account × period × dimension set × currency, materialised and rebuildable

#### Scenario: Approvals and AI proposals
- **WHEN** an approval is decided or an AI proposal is made
- **THEN** each approval decision belongs to its approval request and carries a signed payload hash
- **AND** each AI proposal keeps its items and its AI activity

#### Scenario: Attachments and evidence links
- **WHEN** a file is attached
- **THEN** the attachment records its Cloud Storage object, hash and retention
- **AND** each evidence link records its parent, type, data class, related date and superseded_by

#### Scenario: Equity, settings and help state
- **WHEN** equity, settings and help progress are recorded
- **THEN** the model holds share classes, append-only equity transactions, instruments (SAFE, note or option grant), sandbox round scenarios, director balance movements with their origin, effective-dated setting values marked recommended or changed, and each user's help state (tour, tips)

#### Scenario: Templates, time, claims, providers and bank connections
- **WHEN** templates, time and costs, trips, advances, provider activity and bank connections are recorded
- **THEN** the model holds document templates with their versions, time entries, cost and billing rates, mileage trips, cash advances, provider transactions with their payouts, and bank connections with their sync runs

#### Scenario: Integrations and notifications
- **WHEN** integrations, messages and notifications are recorded
- **THEN** the model holds integration connections (adapter, secret reference, health), outbound messages with their status, notifications, notification preferences, announcements and announcement dismissals

#### Scenario: Company secretary, agents and knowledge base
- **WHEN** company-secretary records, scheduled agents and the organisation knowledge base are recorded
- **THEN** the model holds corporate officers, registrable controllers, agent definitions (pinned skill), agent schedules, agent runs, knowledge-base pages with versions (human_edited, taint, data class), knowledge-base snapshots and lint findings (AI-56)

### Requirement: Keys
Every row SHALL have a time-ordered UUID (PostgreSQL 18 `uuidv7()`), and human document numbers SHALL be kept separate from it, gap-free per sequence where required.

Source: PRD-001 §11.4

#### Scenario: Tenant-scoped uniqueness
- **WHEN** a tenant table is defined
- **THEN** it has `UNIQUE (organisation_id, id)`

#### Scenario: Composite foreign keys
- **WHEN** a tenant table references another tenant table, including audit, evidence and proposal links
- **THEN** the foreign key is composite: `(organisation_id, x_id) REFERENCES x (organisation_id, id)`

#### Scenario: No reference to another organisation's row
- **GIVEN** foreign-key and unique checks bypass row-level security
- **WHEN** a row tries to reference another organisation's row whose UUID it knows
- **THEN** the composite key refuses it

### Requirement: Tenancy and row-level security
Every table SHALL carry `organisation_id` as the first column of every index, with row-level security policies that read a tenant setting the API derives from the authenticated token (tested by QA-ENG-05).

Source: PRD-001 §11.4

#### Scenario: Tenant setting is transaction-scoped
- **WHEN** a transaction starts
- **THEN** its first statement is `set_config('app.org_id', $1, true)`
- **AND** a pooled connection can never carry one tenant's context into the next request

#### Scenario: Missing setting matches nothing
- **WHEN** a query runs without the tenant setting
- **THEN** it matches no rows

#### Scenario: Application roles cannot bypass row-level security
- **WHEN** an application role queries a table
- **THEN** the role is neither the table owner nor `BYPASSRLS`, and the table has `FORCE ROW LEVEL SECURITY`
- **AND** application roles have no TRUNCATE, REFERENCES or ownership on any table

#### Scenario: Sensitive tables add a scope predicate
- **WHEN** payroll or employee-identifier tables are queried
- **THEN** their policies also apply a scope predicate (SEC-02)

#### Scenario: Trust boundary
- **GIVEN** row-level security on an API-set tenant setting stops query mistakes and cross-tenant bugs but not an attacker who controls the application role
- **WHEN** that risk is addressed
- **THEN** the defences are least-privilege roles per service (SEC-03), no ad-hoc SQL, and audit

### Requirement: Global tables
Installation-scoped tables SHALL have no `organisation_id`, SHALL be listed explicitly, and SHALL be read-only to application roles.

Source: PRD-001 §11.4

#### Scenario: Which tables are global
- **WHEN** the global tables are listed
- **THEN** they are user identities and sign-in providers, Super admins, installation announcements, currencies, published statutory pack definitions (§10.6), connector manifests, platform knowledge-base pages and installation audit events

#### Scenario: Pack activation stays a tenant row
- **WHEN** an organisation activates a version of a published statutory pack
- **THEN** the activation is a tenant row (ENG-32/33)

#### Scenario: Reads through views, writes through audited paths
- **WHEN** a service reads a global table
- **THEN** it reads only through a view exposing the columns that service needs
- **AND** writes go through Super admin or release paths and are audited

#### Scenario: Organisation data stays tenant data
- **WHEN** data belongs to an organisation, memberships included
- **THEN** it is kept in tenant tables

### Requirement: Versioning of records
Mutable business records (contacts, accounts, settings) SHALL keep a version counter for optimistic concurrency and a history in the audit trail, and documents SHALL keep immutable versions.

Source: PRD-001 §11.4

#### Scenario: Concurrent change to a mutable record
- **GIVEN** a contact, account or setting changed after a user read it
- **WHEN** the user saves a change based on the old version counter
- **THEN** the save is refused as a stale version

#### Scenario: History of mutable records
- **WHEN** a mutable business record changes
- **THEN** its history is kept in the audit trail

#### Scenario: Document versions are immutable
- **WHEN** a document changes
- **THEN** a new immutable document version is written and earlier versions are unchanged

### Requirement: Insert-only ledger records
Journal entries and lines SHALL be insert-only, with corrections made as new entries, and each entry SHALL record its posting date, recorded time, source document version, rule version and ledger revision.

Source: PRD-001 §11.4

#### Scenario: Correction is a new entry
- **WHEN** a posted entry must be corrected
- **THEN** a new entry is written
- **AND** the original entry and its lines are not changed

#### Scenario: What an entry records
- **WHEN** an entry is posted
- **THEN** it records `posting_date` (accounting time), `recorded_at` (system time), the source document version, the rule version and `ledger_revision` (ENG-46: the order in which entries became visible)

### Requirement: Money columns
Money SHALL be stored as `NUMERIC` with explicit precision and scale per column family (§11.3), with a currency code beside every amount.

Source: PRD-001 §11.4

#### Scenario: Amount with its currency
- **WHEN** an amount is stored
- **THEN** it is a `NUMERIC` with the precision and scale of its column family
- **AND** its currency code is stored beside it

### Requirement: Effective-dated reference data
Parameter packs, account attributes and salary history SHALL use validity periods with PostgreSQL 18 temporal keys (`WITHOUT OVERLAPS`, `PERIOD` foreign keys; needs `btree_gist`, available on Cloud SQL) or an equivalent exclusion constraint.

Source: PRD-001 §11.4

#### Scenario: Overlapping validity is refused
- **WHEN** a version of a parameter pack, account attribute or salary record would overlap another version's validity period
- **THEN** it is refused

#### Scenario: Master-data versions take a ledger revision
- **WHEN** a new version of report-affecting master data (`account_version`, report groups, cash-flow lines, `dimension_value`) takes effect
- **THEN** it records the ledger revision at which it took effect (ENG-46)

#### Scenario: Report re-run as at a revision
- **WHEN** a report is re-run "as at" revision N
- **THEN** it uses the master-data versions with revision ≤ N

### Requirement: Idempotency keys and posting identity
Journal entries, imports and proposal executions SHALL be unique on `(organisation_id, idempotency_key)`, and the system SHALL keep the transport key and the posting identity apart.

Source: PRD-001 §11.4

#### Scenario: Transport key only stops a repeated request
- **WHEN** a request repeats an API `Idempotency-Key` (24 hours per client, FR-INT-08) or a webhook event ID
- **THEN** the repeated request is stopped
- **AND** the transport key serves no other purpose

#### Scenario: Posting identity never expires
- **WHEN** a posting is made
- **THEN** its identity is derived from the source: (source system, source object ID, economic action, source version) for connector postings, and (document ID, document version, rule version) for documents
- **AND** the identity never expires

#### Scenario: Second posting with the same identity
- **GIVEN** a posting exists with a given posting identity
- **WHEN** a second posting with the same identity is attempted
- **THEN** it is rejected

### Requirement: Single posting path
All postings SHALL go through one posting path (`post_entry()`) in the domain layer, with the database triggers as the backstop, not the primary logic.

Source: PRD-001 §11.4

#### Scenario: Every posting uses the one path
- **WHEN** any feature posts to the ledger
- **THEN** it posts through the single `post_entry()` path
- **AND** the database triggers act as the backstop, not the primary logic

### Requirement: Balances are a rebuildable cache
Balance rows SHALL be a cache rebuilt from journal lines, and the nightly job SHALL prove they equal the lines (INV-08).

Source: PRD-001 §11.4

#### Scenario: Nightly proof of equality
- **WHEN** the nightly job runs
- **THEN** it rebuilds balances from the lines and proves they equal the cached balance rows (INV-08)

### Requirement: Accounting date and recorded time
Every ledger line SHALL have an accounting date (the period it belongs to) and a recorded time (when it was written).

Source: PRD-001 §11.6

#### Scenario: Both times are recorded
- **WHEN** a ledger line is written
- **THEN** it carries its accounting date and its recorded time

#### Scenario: Recorded time is informational
- **WHEN** the recorded time is read
- **THEN** it is the transaction's start time
- **AND** it does not give commit order

### Requirement: Reports as at a ledger revision
Reports SHALL be reproducible "as at" any ledger revision (ENG-46).

Source: PRD-001 §11.6

#### Scenario: A past report is reproduced exactly
- **GIVEN** a report was issued on 5 October and back-dated entries were later posted in open periods
- **WHEN** the report is re-run as at the ledger revision it was issued at
- **THEN** it reproduces the issued report exactly (FR-RPT-08)

#### Scenario: As at a time
- **WHEN** a report is requested "as at" a time
- **THEN** it uses the highest ledger revision recorded by that time

### Requirement: Valid time and system time for master data
Master data that affects postings (account attributes, parameter packs) SHALL be effective-dated (valid time) and versioned (system time).

Source: PRD-001 §11.6

#### Scenario: Both time axes are kept
- **WHEN** an account attribute or a parameter pack changes
- **THEN** the new value carries the date it is effective from (valid time)
- **AND** it is kept as a new version with the time it was recorded (system time)

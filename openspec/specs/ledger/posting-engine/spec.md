# ledger/posting-engine Specification

## Purpose
How source documents become journal entries: a pure, versioned posting engine that posts in the same transaction as the document change, records where every entry came from, feeds every ledger figure through one balance service, and orders postings with a ledger revision and a fixed lock protocol. Derived from PRD-001 §10.1.

## Requirements

### Requirement: ENG-01 Posting engine is a pure function
The posting engine SHALL be a pure function from the document version, the parameters in force at the document's tax point or pay month (ENG-31, ENG-45) and the organisation settings to journal entries.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Same input, same output
- **GIVEN** a document version, the parameters in force and the organisation settings
- **WHEN** the engine posts them twice
- **THEN** it produces the same journal entries both times

#### Scenario: Parameters at the tax point or pay month
- **WHEN** the engine posts a document
- **THEN** it uses the parameters in force at the document's tax point or pay month (ENG-31, ENG-45)

#### Scenario: No clock, randomness or I/O inside rules
- **WHEN** a posting rule runs
- **THEN** it reads no clock, uses no randomness and performs no I/O

### Requirement: ENG-02 Posting in the document's transaction
Posting SHALL happen in the same database transaction as the document change, so the ledger is never eventually consistent with its documents.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Document change and entries commit together
- **WHEN** a document change that posts is committed
- **THEN** its journal entries are committed in the same database transaction

#### Scenario: Ledger never lags its documents
- **WHEN** another transaction reads a changed document and the ledger
- **THEN** it sees either both the document change and its entries, or neither

### Requirement: ENG-03 Entry and line provenance
Every journal entry SHALL reference its source and the rule version that produced it and carry its ledger revision (ENG-46), and every line SHALL carry its amounts, currency, rate, dimensions and dates.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Entry references its source, rule version and revision
- **WHEN** a journal entry is posted
- **THEN** it references its source (document type, ID, version) and the rule version that produced it
- **AND** it carries its ledger revision (ENG-46)

#### Scenario: Line carries its amounts and dates
- **WHEN** a journal line is posted
- **THEN** it carries the account, amount, currency, base amount, FX rate and source, dimensions, posting date (accounting date) and recorded time

#### Scenario: A base-currency line's rate
- **WHEN** a journal line in the base currency is posted
- **THEN** it stores rate 1 with source "base"
- **AND** a rate entered on it is refused with a field error

#### Scenario: A converted line always has a source
- **WHEN** a journal line not in the base currency is posted
- **THEN** it carries a rate and its source
- **AND** a rate typed on the document has the source "document" (ENG-40)

### Requirement: ENG-04 Single source of truth
Every report, KPI, dashboard tile, check and AI answer SHALL read the ledger through the balance service, and no report SHALL re-implement posting logic.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Ledger figures come only from the balance service
- **WHEN** a ledger figure (a balance, movement, statement or subledger total) is shown anywhere
- **THEN** it comes from the balance service
- **AND** nothing else sums journal lines

#### Scenario: Measures from other sources
- **WHEN** a measure comes from another source (subscriptions, time entries, the equity register, payroll headcount, open documents' expected dates, benchmark packs)
- **THEN** it is a named measure in the semantic layer (AI-03) that declares its source (FR-KPI-03)
- **AND** it never posts and is never presented as a ledger figure

### Requirement: ENG-05 Scheduled recognition up to the current period
The scheduler SHALL post scheduled recognition (spreads, deferrals, depreciation, auto-reversals) as real journal entries up to the current period.

Source: PRD-001 §10.1 · Priority M · Phase P1

#### Scenario: Posted up to the current period only
- **GIVEN** a schedule that runs past the current period
- **WHEN** the scheduler runs
- **THEN** real journal entries are posted for the periods up to the current period
- **AND** no entry is posted for a future period

#### Scenario: Future views are projections
- **WHEN** an "as of" view of a future period is shown
- **THEN** its scheduled amounts are computed and flagged as projections
- **AND** they are never mixed with actuals

#### Scenario: Built for bills' spreads (rev 0.83)
- **GIVEN** a posted bill with a line spread over months
- **WHEN** a release's date comes in the organisation's time zone
- **THEN** it posts under the scheduler's principal, at once when the bill posts or is edited and otherwise by the hourly job, and one whose date no open period covers waits with an audit event
- **AND** the bill lists each release as posted, waiting, projected (not in the ledger) or reversed

### Requirement: ENG-06 Versioned posting rules
Rule changes SHALL be versioned, and re-running a rule version over the same document SHALL reproduce the original entries exactly.

Source: PRD-001 §10.1 · Priority M · Phase P1

#### Scenario: A rule change is a new version
- **WHEN** a posting rule changes
- **THEN** the change is a new rule version

#### Scenario: Replay reproduces the original entries
- **GIVEN** a document posted under a rule version
- **WHEN** that rule version is re-run over the same document (replay test, QA-ENG-06)
- **THEN** it reproduces the original entries exactly

#### Scenario: Every rule version is kept and replayed (rev 0.67)
- **GIVEN** the rule versions the engine has posted with
- **WHEN** a build runs
- **THEN** each version replays its fixed documents and must match its reviewed golden output
- **AND** an entry under a version the engine no longer has is refused by the replay (LEDGER-18)

### Requirement: ENG-46 Ledger revision
Each organisation SHALL have one ledger revision counter, and every journal entry and every change to master data that reports show SHALL take the next revision.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Revision assigned at insert under a lock
- **WHEN** a journal entry is inserted
- **THEN** the database assigns it the next revision under a row lock held until commit

#### Scenario: Revisions visible in order without gaps
- **GIVEN** postings that take revisions, one of which is aborted
- **WHEN** the others commit
- **THEN** the revisions become visible in order
- **AND** the aborted posting leaves no gap

#### Scenario: Master-data changes take a revision
- **WHEN** master data that reports show changes (account names, report groups, cash-flow lines, dimension values)
- **THEN** the change takes the next revision

#### Scenario: As at revision N
- **WHEN** figures are read "as at revision N"
- **THEN** they include every entry with revision ≤ N
- **AND** that set never changes

#### Scenario: A report's first run records its revision
- **WHEN** a report runs for the first time
- **THEN** it reads the revision and its figures in one snapshot (one statement, or a read-only REPEATABLE READ transaction)
- **AND** it records that N

#### Scenario: Times, keys and sequences are not revisions
- **WHEN** the set of entries "as at" a point is determined
- **THEN** recorded times, UUIDv7 keys and sequences are not used, because they are taken before commit

### Requirement: ENG-47 Ledger concurrency protocol
Database transactions SHALL run at READ COMMITTED with explicit row locks always taken in one fixed order, so that a posting and a period close never interleave.

Source: PRD-001 §10.1 · Priority M · Phase P0

#### Scenario: Lock order
- **WHEN** a command takes row locks
- **THEN** it locks (1) the source documents it changes, by ID; (1a) the number sequence of a document type, when it numbers a document; (2) the organisation's ledger-revision row (ENG-46), when the first entry is inserted, as the last step of the command; (3) period rows, by date

#### Scenario: Posting checks the period after the revision lock
- **WHEN** a posting checks the period status
- **THEN** it does so after it holds the revision lock

#### Scenario: Close and reopen
- **WHEN** a period is closed or reopened
- **THEN** the command takes the same revision lock, then the period row `FOR UPDATE`, then changes the status

#### Scenario: Posting and close never interleave
- **GIVEN** a posting and a close of the same period at the same time
- **WHEN** both run
- **THEN** one commits first and the other sees it

#### Scenario: Payment allocation locks the open document
- **WHEN** a payment is allocated
- **THEN** the open document's row is locked before its open balance is read

#### Scenario: Deadlocks and serialisation failures are retried
- **WHEN** a request hits a deadlock (SQLSTATE 40P01) or a serialisation failure (40001)
- **THEN** the API retries it with the same Idempotency-Key, up to 3 times

#### Scenario: Stable error after retries or a long lock wait
- **GIVEN** the retries are used up, or a lock wait exceeds 5 s (setting)
- **WHEN** the request ends
- **THEN** the caller gets a stable error code (ENG-23)

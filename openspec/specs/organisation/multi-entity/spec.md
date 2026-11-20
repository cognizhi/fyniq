# organisation/multi-entity Specification

## Purpose
How legal entities, each its own Fyniq organisation, are linked in a group by the consent of both Owners and consolidated, and how an accounting firm manages many client organisations (rev 0.625: an entity is an organisation, not a level inside one). Derived from PRD-001 §8.20.

## Requirements

### Requirement: FR-ENT-01 Several legal entities per organisation
The system SHALL let several legal entities, each an organisation with its own base currency, chart of accounts, tax packs and periods, belong to one group (rev 0.625: an entity is an organisation).

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Entity settings are its own
- **GIVEN** two organisations in one group
- **WHEN** each one's base currency, chart of accounts, tax packs and periods are set
- **THEN** each keeps its own values, independent of the other, and there is no entity level inside an organisation

### Requirement: FR-ENT-02 Intercompany and consolidation
The system SHALL record intercompany transactions and produce consolidations with eliminations and currency translation.

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Intercompany transaction
- **WHEN** a transaction between two entities of the organisation is recorded
- **THEN** it is recorded as an intercompany transaction in both entities

#### Scenario: Consolidation
- **WHEN** a consolidation of the organisation's entities is produced
- **THEN** intercompany transactions are eliminated
- **AND** each entity's figures are translated into the consolidation currency

### Requirement: FR-ENT-03 Accountant-practice mode
The system SHALL offer an accountant-practice mode in which a firm manages many client organisations, with per-client access and a practice dashboard.

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Per-client access
- **GIVEN** an accounting firm in firm mode
- **WHEN** a member of the firm is given access to a client organisation
- **THEN** the access applies to that client only

#### Scenario: Practice dashboard
- **WHEN** a member of the firm opens the practice dashboard
- **THEN** it shows the client organisations they manage

### Requirement: FR-ENT-04 Groups and the link between organisations
The system SHALL link organisations in a group only by the consent of both Owners, read-only and narrow, with each side able to end the link at once (rev 0.625).

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: A link needs both Owners
- **WHEN** the parent's Owner issues a one-time invitation and the entity's Owner accepts it in the entity's organisation, each with step-up
- **THEN** each organisation holds a row of the link in its own audit trail and a read needs both rows active
- **AND** the code works once, within 24 hours

#### Scenario: What the group may read
- **WHEN** the group reads an entity
- **THEN** it reads each account's net movement by month in the entity's base currency with its code, name and type, the ledger revision and each period's state, and intercompany offers addressed to the entity
- **AND** it reads no document, contact, employee, bank, evidence, comment, audit or AI data, and writes nothing

#### Scenario: Ending the link
- **WHEN** either Owner ends the link with a reason
- **THEN** the next read is refused and every read so far is in both audit trails
- **AND** an organisation belongs to one group at a time and a sandbox, a practice or the installation's organisation is never an entity

### Requirement: FR-ENT-05 Account mapping and translation rates
The system SHALL map each entity account to an account of the parent's chart and keep the group's own translation rates, a closing and an average per month and currency (rev 0.625).

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Mapping
- **WHEN** a run finds an account with a balance and no mapping
- **THEN** the run is blocked and names the account
- **AND** a mapping may name a counterparty entity for an intercompany account

#### Scenario: Rates
- **WHEN** a rate is entered for a month and currency
- **THEN** it keeps its source note and a newer entry corrects it without deleting it
- **AND** a month with no rate blocks the run and the entity's own FX rates are never borrowed

### Requirement: FR-ENT-06 The consolidation run
The system SHALL translate each entity's figures into the group currency, eliminate mirror accounts pair by pair and freeze the result, showing every difference and plugging none (rev 0.625).

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Translation
- **WHEN** a run translates an entity
- **THEN** assets and liabilities are at the closing rate of the last month, each month's revenue and expense at that month's average rate and each month's equity movement at that month's closing rate, each line under RND-TRANSLATE
- **AND** the translation reserve is the figure that balances the entity's translated trial balance to nothing

#### Scenario: Elimination
- **WHEN** two entities carry mirror amounts
- **THEN** the smaller translated amount is eliminated from both and the remainder stays with the run naming the larger side and the difference
- **AND** nothing is posted to any entity's ledger

#### Scenario: Frozen and provisional
- **WHEN** a run is made
- **THEN** it keeps the figures read, the rates, the entities' ledger revisions and period states and is never changed, and a period not yet closed makes it provisional
- **AND** after a link ends its runs stay marked as link ended

### Requirement: FR-ENT-07 Accountant-firm mode and the practice dashboard
The system SHALL let a firm's Owner invite clients that accept by their own Owner, and show the firm's people a dashboard of counts without money for the clients where they already hold a role (rev 0.625).

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Engagement grants nothing
- **WHEN** a client's Owner accepts a firm's invitation
- **THEN** an engagement naming the firm is recorded in the client's books and no person gains any right by it
- **AND** per-client access is the membership, role and expiry the client's Owner gives

#### Scenario: The dashboard
- **WHEN** a member of the firm opens the practice dashboard
- **THEN** it lists the clients where they hold an active membership and an engagement names the firm, with counts that carry no money
- **AND** a count their role there may not see shows as not allowed

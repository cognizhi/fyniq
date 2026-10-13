# organisation/multi-entity Specification

## Purpose
How one Fyniq organisation keeps the books of several legal entities and consolidates them, and how an accounting practice manages many client organisations. Derived from PRD-001 §8.20.

## Requirements

### Requirement: FR-ENT-01 Several legal entities per organisation
The system SHALL let an organisation hold several legal entities, each with its own base currency, chart of accounts mapping, tax packs and periods.

Source: PRD-001 §8.20 · Priority C · Phase P3

#### Scenario: Entity settings are its own
- **GIVEN** an organisation with two legal entities
- **WHEN** each entity's base currency, chart of accounts mapping, tax packs and periods are set
- **THEN** each entity keeps its own values, independent of the other

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
- **GIVEN** an accounting firm in practice mode
- **WHEN** a member of the firm is given access to a client organisation
- **THEN** the access applies to that client only

#### Scenario: Practice dashboard
- **WHEN** a member of the firm opens the practice dashboard
- **THEN** it shows the client organisations they manage

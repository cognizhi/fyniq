# ai/data-access Specification

## Purpose
What the AI assistant may read and how: on-behalf-of authorisation, the AI exclusion policy, read tools on a curated semantic layer, result limits, figures returned as typed facts, and where model calls are processed. Derived from PRD-001 §9.4.

## Requirements

### Requirement: AI-01 On-behalf-of authorisation
The AI SHALL see only what the signed-in user can see, intersected with the AI exclusion policy.

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: Records the user cannot see stay hidden from the AI
- **GIVEN** a record the signed-in user is not allowed to see
- **WHEN** the AI reads data on that user's behalf
- **THEN** the record is not returned to the AI

#### Scenario: Excluded data the user can see stays hidden from the AI
- **GIVEN** data the signed-in user can see that the AI exclusion policy excludes
- **WHEN** the AI reads data on that user's behalf
- **THEN** the excluded data is not returned to the AI

### Requirement: AI-02 AI exclusions
The system SHALL apply an AI exclusion policy (the default, Owner-configurable, dual-controlled) that never sends NRIC/FIN, full bank account numbers, passwords, secrets, authentication data or full home addresses to the model, and sends payroll only when both the organisation and the user allow it.

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: Identity, bank, credential and address data never reach the model
- **WHEN** data the AI reads contains NRIC/FIN, a full bank account number, a password, a secret, authentication data or a full home address
- **THEN** that data is never sent to the model

#### Scenario: Payroll with AI payroll access and the payroll role
- **GIVEN** the organisation has enabled "AI payroll access"
- **AND** the user has the payroll role
- **WHEN** the AI reads payroll data on that user's behalf
- **THEN** payroll and individual salaries may be sent to the model

#### Scenario: Payroll withheld without both conditions
- **GIVEN** "AI payroll access" is not enabled for the organisation, or the user does not have the payroll role
- **WHEN** the AI reads data on that user's behalf
- **THEN** payroll and individual salaries are not sent to the model

#### Scenario: Changing the exclusion policy is dual-controlled
- **WHEN** the Owner changes the AI exclusion policy
- **THEN** the change needs dual control

#### Scenario: The never-send items cannot be removed
- **WHEN** the Owner edits the exclusion policy
- **THEN** they may add exclusions but cannot remove NRIC/FIN, full bank account numbers, passwords, secrets, authentication data or full home addresses
- **AND** the payroll switch is the only setting that widens what is sent

### Requirement: AI-03 Curated semantic layer
Read tools SHALL query a curated semantic layer of named measures and dimensions (e.g. `revenue`, `net_burn`, `ar_outstanding` by `customer`, `month`), compiled to SQL by platform code.

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: Platform code compiles the query
- **WHEN** a read tool asks for a named measure by named dimensions, such as `ar_outstanding` by `customer`
- **THEN** platform code compiles the request to SQL

#### Scenario: The model writes no SQL against production in P1
- **GIVEN** phase P1
- **WHEN** the AI needs data
- **THEN** the model never writes SQL against production

#### Scenario: Read-only SQL sandbox is a P2 option for the Accountant role
- **GIVEN** phase P2 and the read-only SQL sandbox option is used for a user with the Accountant role
- **WHEN** a query runs in the sandbox
- **THEN** it runs read-only over the reporting dataset, through BigQuery authorised views with the same row-level policy

### Requirement: AI-04 Result limits
Every read tool call SHALL be bounded by row caps, column allow-lists, statement timeouts and token budgets per tool call.

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: Limits apply to every tool call
- **WHEN** a read tool call runs
- **THEN** it is limited by its row cap, its column allow-list, its statement timeout and its token budget

#### Scenario: Large results are summarised by the tool
- **WHEN** a read tool's result is large
- **THEN** the tool summarises it
- **AND** the full result is not dumped into the conversation

### Requirement: AI-68 Typed facts
Every figure a read tool returns SHALL be a fact with an ID, the measure, its dimensions and filters, the period or as-at date, currency and unit, sign convention, the ledger revision (ENG-46) and a lineage link (`lineage.explain`).

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: A figure carries its fact attributes
- **WHEN** a read tool returns a figure
- **THEN** it is a fact with an ID, the measure, its dimensions and filters (entity, customer, account, project …), the period or as-at date, currency and unit, sign convention, the ledger revision (ENG-46) and a lineage link (`lineage.explain`)

#### Scenario: Calculator results name their input facts
- **WHEN** the `calc` tool returns a result
- **THEN** the result carries the IDs of its input facts

#### Scenario: Counts, dates, percentages and statutory values are typed
- **WHEN** a read tool returns a count, a date, a percentage or a statutory value
- **THEN** the value is typed as such
- **AND** a statutory value carries its pack key and version (AI-53)

#### Scenario: MCP results carry the same facts
- **WHEN** a read tool is called over MCP
- **THEN** its figures are returned as the same typed facts (AI-41)

### Requirement: AI-05 Data residency and provider terms
Model calls SHALL go to Google Cloud's Agent Platform under Google Cloud's terms, with the endpoint (Singapore, global or multi-region) and the model following ADR-T2 (§12.5).

Source: PRD-001 §9.4 · Priority M · Phase P1

#### Scenario: Endpoint and model follow ADR-T2
- **WHEN** the assistant calls a model
- **THEN** the call goes to Google Cloud's Agent Platform under Google Cloud's terms
- **AND** the endpoint and the model are the ones ADR-T2 (§12.5) selects

#### Scenario: Cross-border processing is assessed and recorded
- **GIVEN** the configured endpoint or model processes data outside Singapore
- **WHEN** it is configured
- **THEN** the cross-border processing is assessed under PDPA's Transfer Limitation Obligation
- **AND** it is recorded in the data inventory (§14)

#### Scenario: G4-087 Pre-go-live check of the model IDs and endpoint
- **GIVEN** the configured model IDs and endpoint
- **WHEN** they are checked before go-live
- **THEN** dated captures of the Google pages and a synthetic call to `asia-southeast1` succeed for each configured model
- **AND** the organisation policy blocks the global endpoint
- **AND** the data inventory says the AI path covers model calls only

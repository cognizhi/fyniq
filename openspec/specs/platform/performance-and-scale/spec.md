# platform/performance-and-scale Specification

## Purpose
The performance, scale, availability, API usability and browser targets Fyniq is built to meet: latency of reads, cold starts, reports, posting, imports and exports; volume per organisation and tenants; monthly availability; fully documented API endpoints; and the supported browsers. Derived from PRD-001 §15.

## Requirements

### Requirement: NFR-PERF-01 Interactive read latency
The system SHALL serve interactive reads (record, list page, search) at p95 ≤ 300 ms server time.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Interactive reads measured
- **WHEN** the server time of record reads, list pages and searches is measured
- **THEN** the 95th percentile is ≤ 300 ms

### Requirement: NFR-PERF-06 Cold start
The first API request after idle SHALL add p95 ≤ 2 s to NFR-PERF-01 [proposed, confirmed by a P0 measurement], and the other cold-start paths SHALL be measured and reported in P1, not gated.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Measured on the real images in staging
- **WHEN** cold start is measured
- **THEN** it is the first request after idle, measured on the real images in staging

#### Scenario: API record read after idle
- **WHEN** an API record read is the first request after idle
- **THEN** its 95th percentile adds ≤ 2 s to the NFR-PERF-01 target [proposed, confirmed by a P0 measurement]

#### Scenario: Other paths reported, not gated
- **WHEN** a sign-in session check, an OAuth token issue, a first AI token or an upload scan is the first request after idle
- **THEN** its time is measured and reported in P1
- **AND** it does not gate a release

### Requirement: NFR-PERF-02 Standard report time
The system SHALL produce a standard report (P&L, BS, TB, ageing) for 12 months with comparatives at p95 ≤ 2 s at 1 M ledger lines per organisation, and ≤ 5 s at 10 M.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Report at 1 M ledger lines
- **GIVEN** an organisation with 1 M ledger lines
- **WHEN** a P&L, BS, TB or ageing report for 12 months with comparatives is run
- **THEN** the 95th percentile time is ≤ 2 s

#### Scenario: Report at 10 M ledger lines
- **GIVEN** an organisation with 10 M ledger lines
- **WHEN** the same report is run
- **THEN** the 95th percentile time is ≤ 5 s

### Requirement: NFR-PERF-03 Posting time
The system SHALL save and post a 50-line document at p95 ≤ 500 ms.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: 50-line document posted
- **WHEN** a 50-line document is saved and posted
- **THEN** the 95th percentile time is ≤ 500 ms

### Requirement: NFR-PERF-04 Bank import time
The system SHALL import, de-duplicate and rule-match 10,000 bank lines in ≤ 60 s, asynchronously with progress.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: 10,000 bank lines imported
- **WHEN** 10,000 bank lines are imported
- **THEN** they are imported, de-duplicated and rule-matched in ≤ 60 s
- **AND** the import runs asynchronously and shows its progress

### Requirement: NFR-PERF-05 GL export time
The system SHALL export a 1 M-line GL to CSV/XLSX in ≤ 5 min, asynchronously and streamed.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: 1 M-line GL export
- **WHEN** a GL of 1 M lines is exported to CSV or XLSX
- **THEN** the export finishes in ≤ 5 min
- **AND** it runs asynchronously and is streamed

### Requirement: NFR-SCALE-01 Volume per organisation
Each organisation SHALL support 10 M ledger lines, 100 concurrent users and 5 years of online history without re-architecture.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Volume reached without re-architecture
- **GIVEN** an organisation with 10 M ledger lines, 100 concurrent users and 5 years of online history
- **WHEN** it uses the platform
- **THEN** the platform serves it without re-architecture

### Requirement: NFR-SCALE-02 Tenants
Multi-tenant SaaS (P3) SHALL run on the same architecture, with 1,000 organisations on a shared database tier and an option for a dedicated database per large tenant.

Source: PRD-001 §15 · Priority M · Phase P3

#### Scenario: Shared database tier
- **WHEN** 1,000 organisations use the multi-tenant SaaS
- **THEN** they are served on a shared database tier on the same architecture

#### Scenario: Dedicated database for a large tenant
- **WHEN** a large tenant needs its own database
- **THEN** a dedicated database can be provided for it

### Requirement: NFR-AVAIL-01 Availability
The platform SHALL be available ≥ 99.9% monthly at P1 and ≥ 99.95% at P3, excluding announced maintenance.

Source: PRD-001 §15 · Priority M · Phase P1 / P3

#### Scenario: Monthly availability at P1
- **WHEN** availability is measured over a month at P1, excluding announced maintenance
- **THEN** it is ≥ 99.9%

#### Scenario: Monthly availability at P3
- **WHEN** availability is measured over a month at P3, excluding announced maintenance
- **THEN** it is ≥ 99.95%

### Requirement: NFR-API-01 Documented endpoints
Every endpoint SHALL have a description, an example request and response, its permission and its error codes in the OpenAPI document, for 100% of endpoints.

Source: PRD-001 §15 · Priority M · Phase P0

#### Scenario: Linter checks every endpoint
- **WHEN** CI runs
- **THEN** a linter (Spectral) checks that 100% of endpoints have a description, an example request and response, their permission and their error codes in the OpenAPI document

### Requirement: NFR-BROWSER-01 Supported browsers
The web app SHALL support the last two versions of Chrome, Edge, Safari and Firefox, and iOS Safari and Android Chrome for the PWA.

Source: PRD-001 §15 · Priority M · Phase P1

#### Scenario: Desktop browsers
- **WHEN** the web app is used in one of the last two versions of Chrome, Edge, Safari or Firefox
- **THEN** it works

#### Scenario: PWA on mobile
- **WHEN** the PWA is used on iOS Safari or Android Chrome
- **THEN** it works

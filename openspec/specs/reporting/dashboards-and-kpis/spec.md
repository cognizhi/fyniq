# reporting/dashboards-and-kpis Specification

## Purpose
How Fyniq shows the business at a glance: the default Home and Dashboard tiles, the KPI set and its versioned definitions with honest undefined states, custom dashboards, anomaly alerts, and industry and peer benchmarks. Derived from PRD-001 §8.13.

## Requirements

### Requirement: FR-KPI-01 Default Home and Dashboard
The system SHALL provide the workbook's 16 dashboard tiles and 4 charts as the default Home and Dashboard.

Source: PRD-001 §8.13 · Priority M · Phase P1

#### Scenario: Default tiles and charts
- **GIVEN** a user who has not customised Home or the Dashboard
- **WHEN** they open Home or the Dashboard
- **THEN** they see the workbook's 16 tiles and 4 charts: cash, burn, runway, revenue, gross margin, net profit YTD, owed to us, owed by us, revenue vs budget, cost vs budget (two tiles, which makes the 16), forecast cash-out date, books health, pipeline, unbilled time, recurring due and unrealised FX

### Requirement: FR-KPI-02 Workbook KPI set
The system SHALL provide the workbook KPI set with its definitions (workbook PRD §11.5), including two MRR measures, each named on every tile that uses it; open question 34 picks the headline MRR.

Source: PRD-001 §8.13 · Priority M · Phase P1

#### Scenario: KPIs provided
- **WHEN** the KPI page is opened
- **THEN** it offers MRR, ARR, growth, gross margin, spend by function, net burn, runway, ARPA, churn, CAC, LTV, LTV:CAC, CAC payback, burn multiple, current and quick ratio, working capital, debt/equity, DSO, DPO, EBITDA margin, net margin, headcount and revenue per employee, each with its definition

#### Scenario: Booked MRR
- **WHEN** booked MRR is computed for a month
- **THEN** it is the subscription revenue recognised in that month, from the ledger (the workbook definition)

#### Scenario: Contracted MRR
- **WHEN** contracted MRR is computed for a month
- **THEN** it is the monthly value of the subscriptions active at month end (FR-AR-15, FR-PSP-03, FR-KPI-04)

#### Scenario: Each tile names its MRR measure
- **WHEN** a tile uses MRR
- **THEN** it names whether it shows booked MRR or contracted MRR

#### Scenario: Both measures explained
- **WHEN** the KPI page is opened
- **THEN** it shows both MRR measures
- **AND** it explains the difference between them (mid-month starts, credits, unpaid or failed invoices, FX)

#### Scenario: Customer counts and headcount entered until their data exists (rev 0.161)
- **WHEN** no subscription data (FR-KPI-04) or payroll link yet gives a month's customer counts or headcount
- **THEN** a person enters the month's paying customers, new customers, customers lost and headcount, audited
- **AND** each KPI that uses them is labelled entered, and the entries give way to the subscriptions' counts and the payroll link's headcount when those arrive

#### Scenario: G4-046 Mid-month start and an unpaid invoice
- **GIVEN** a subscription starts on the 20th, and one subscription invoice is unpaid at month end
- **WHEN** the month's MRR is shown
- **THEN** booked MRR and contracted MRR differ
- **AND** the KPI page shows both and lists the reconciling items

### Requirement: FR-KPI-03 KPI definitions as versioned code
KPI definitions SHALL be versioned code with tests, and each tile SHALL show its definition on hover.

Source: PRD-001 §8.13 · Priority M · Phase P1

#### Scenario: Definitions are versioned and tested
- **WHEN** a KPI definition changes
- **THEN** the change is a new version of the definition code, covered by tests

#### Scenario: Definition shown on hover
- **WHEN** a user hovers over a KPI tile
- **THEN** the tile shows the KPI's definition

#### Scenario: What a definition states
- **WHEN** a KPI definition is read
- **THEN** it states its source (the ledger through the balance service, or a named operational source), formula, date basis (accounting period, service period or as-at date), currency and rounding, and its undefined state

#### Scenario: Undefined KPIs show a dash with the reason
- **GIVEN** a KPI whose denominator is zero, or whose source data is missing
- **WHEN** it is shown
- **THEN** it shows "—" with the reason (for example "no subscription data"), never 0

#### Scenario: Undefined states carried over from the workbook
- **WHEN** runway, the burn multiple, debt/equity and the margins are shown
- **THEN** runway shows "Not burning" when 3-month net burn ≤ 0
- **AND** the burn multiple is blank when not burning
- **AND** debt/equity is blank when equity ≤ 0
- **AND** the margins are blank when revenue is 0

#### Scenario: G4-047 No subscription or CRM data
- **GIVEN** no subscription or CRM data is connected
- **WHEN** the KPIs are shown
- **THEN** churn, ARPA, LTV and CAC show "—" with "no subscription data", never 0
- **AND** the ledger KPIs still show
- **AND** (rev 0.161) with the month's customer counts entered, they show, labelled entered

#### Scenario: G4-048 Negative equity in a cash-generating month
- **GIVEN** equity ≤ 0 and a month that generates cash
- **WHEN** the KPIs are shown
- **THEN** debt/equity is blank
- **AND** runway shows "Not burning"
- **AND** the burn multiple is blank

### Requirement: FR-KPI-04 Customer counts from subscriptions or integrations
Customer counts for SaaS KPIs SHALL come from the subscription schedules (FR-AR-15) or a CRM/billing integration instead of manual input.

Source: PRD-001 §8.13 · Priority S · Phase P2

#### Scenario: Counts come from records
- **WHEN** a SaaS KPI needs a customer count
- **THEN** the count comes from the subscription schedules (FR-AR-15) or a CRM/billing integration, not from manual input

### Requirement: FR-KPI-05 Custom dashboards
The system SHALL support custom dashboards per role and per user (FR-VIZ-09).

Source: PRD-001 §8.13 · Priority S · Phase P2

#### Scenario: Dashboard per role
- **GIVEN** a custom dashboard set up for a role
- **WHEN** a user with that role opens the Dashboard
- **THEN** they see that role's dashboard

#### Scenario: Dashboard per user
- **WHEN** a user saves their own custom dashboard
- **THEN** it is kept for that user

### Requirement: FR-KPI-06 Anomaly alerts
The system SHALL raise anomaly alerts as Inbox items.

Source: PRD-001 §8.13 · Priority C · Phase P2

#### Scenario: Spend above its trend
- **WHEN** spend in a category is 3σ above its trend
- **THEN** an anomaly alert is raised as an Inbox item

### Requirement: FR-KPI-07 Industry benchmarks
The system SHALL show KPIs against industry bands for the organisation's industry code (SSIC) and size, from benchmark packs: sourced, licensed and dated data sets kept like statutory packs (§10.6).

Source: PRD-001 §8.13 · Priority S · Phase P2

#### Scenario: KPIs compared with industry bands
- **WHEN** industry benchmarks are shown
- **THEN** gross margin, opex ratios by function, DSO, DPO, revenue per employee, burn multiple and growth are shown against the bands for the organisation's SSIC code and size

#### Scenario: Benchmark packs
- **WHEN** a benchmark pack is added
- **THEN** it is a sourced, licensed and dated data set kept like a statutory pack (§10.6), for example Singapore Department of Statistics industry surveys and published SaaS benchmark reports [verify licences]

#### Scenario: Each comparison shows its provenance
- **WHEN** a benchmark comparison is shown
- **THEN** it shows the source, the year and the sample

#### Scenario: Benchmarks never touch the ledger
- **WHEN** benchmarks are loaded or compared
- **THEN** nothing is posted to or changed in the ledger

#### Scenario: AI explains a gap
- **WHEN** a user asks the AI about a gap to a benchmark
- **THEN** the AI may explain it in the read lane

### Requirement: FR-KPI-08 Peer benchmarks
The system SHALL offer opt-in, anonymised peer benchmarks across organisations on the platform (SaaS, P3), under a PDPA assessment and the data inventory, publishing only cells that pass the disclosure-control rules.

Source: PRD-001 §8.13 · Priority C · Phase P3

#### Scenario: Only organisations that opt in
- **GIVEN** an organisation has not opted in
- **WHEN** peer benchmarks are computed
- **THEN** its data is not used

#### Scenario: Minimum cell size
- **WHEN** a peer-benchmark cell has fewer than 10 organisations
- **THEN** it is not published

#### Scenario: No dominant organisations
- **WHEN** one or two organisations dominate a cell
- **THEN** the cell is not published, whatever its count

#### Scenario: Complementary suppression
- **WHEN** a hidden cell could be worked out from totals or other filters
- **THEN** complementary suppression hides the cells that would reveal it

#### Scenario: Rules set in the PDPA assessment
- **WHEN** the disclosure-control rules and thresholds are set
- **THEN** they are set in the PDPA assessment using a recognised statistical disclosure-control method [verify]

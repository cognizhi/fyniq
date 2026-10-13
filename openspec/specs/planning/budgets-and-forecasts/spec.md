# planning/budgets-and-forecasts Specification

## Purpose
How Fyniq plans ahead: budgets, the driver-based and rolling forecasts and their outputs, the short-term and 13-week cash projections, the feasibility calculator and the funding plan, all sandboxed so that no plan ever posts to the ledger. Derived from PRD-001 §8.14.

## Requirements

### Requirement: FR-PLN-01 Budgets
The system SHALL keep monthly budgets by account and dimension, in several versions (original, revised), with approval and lock.

Source: PRD-001 §8.14 · Priority M · Phase P1

#### Scenario: Monthly budget by account and dimension
- **WHEN** a budget is entered
- **THEN** it holds an amount per month per account and dimension

#### Scenario: Several versions
- **GIVEN** an original budget
- **WHEN** a revised version is created
- **THEN** both versions are kept

#### Scenario: Approval and lock
- **WHEN** a budget version is approved
- **THEN** it can be locked against further change

#### Scenario: Copy with growth
- **WHEN** a budget is copied with a growth rate
- **THEN** the new version holds the copied amounts adjusted by that growth

#### Scenario: Import from XLSX
- **WHEN** a budget is imported from an XLSX file
- **THEN** its amounts are loaded into a budget version

### Requirement: FR-PLN-02 Driver-based forecast
The system SHALL provide a driver-based forecast (the workbook model) with scenarios (Base, Best, Worst and custom) over a horizon of 12–60 months.

Source: PRD-001 §8.14 · Priority M · Phase P1

#### Scenario: Scenarios and horizon
- **WHEN** a forecast is set up
- **THEN** it has Base, Best and Worst scenarios and can add custom ones
- **AND** its horizon is between 12 and 60 months

#### Scenario: Revenue drivers
- **WHEN** revenue is forecast
- **THEN** the drivers are customers, new customers and growth, churn, ARPA, price increases, services, other revenue and the collection pattern

#### Scenario: Cost drivers
- **WHEN** costs are forecast
- **THEN** the drivers are hosting %, AI/API %, contractors %, payment fees %, salary increases and other opex

#### Scenario: Plans and levers
- **WHEN** a scenario is built
- **THEN** it includes a headcount plan, a capex plan, a funding plan and scenario levers

### Requirement: FR-PLN-03 Forecast outputs
The system SHALL produce the outputs of each forecast scenario and compare scenarios side by side.

Source: PRD-001 §8.14 · Priority M · Phase P1

#### Scenario: Outputs of a scenario
- **WHEN** a scenario is calculated
- **THEN** it produces a monthly P&L and cash, a yearly summary, a tax estimate (SUTE/PTE), the cash-out month, runway, minimum cash, the break-even month, average burn, the funding gap for the buffer, next-12-month revenue, and ending ARR and customers

#### Scenario: Side-by-side comparison
- **WHEN** several scenarios are compared
- **THEN** their outputs are shown side by side

#### Scenario: Funding not yet received
- **GIVEN** a scenario's funding plan includes money not yet received
- **WHEN** its runway and cash-out month are shown
- **THEN** both are shown with and without that money

#### Scenario: Home, tiles and alerts use confirmed funding only
- **GIVEN** a scenario's funding plan includes money not yet received
- **WHEN** Home, the KPI tiles and alerts show runway or the cash-out month
- **THEN** they use the figure without the unconfirmed funding and say so

#### Scenario: G4-108 Seed round not yet received
- **GIVEN** a forecast's funding plan includes a seed round not yet received
- **WHEN** runway and the cash-out month are shown
- **THEN** they are shown with and without the round
- **AND** Home and alerts use the figure without it and say so

### Requirement: FR-PLN-04 Rolling forecast
The system SHALL keep the forecast rolling, replacing forecast months with actuals automatically as periods close, and SHALL show how accurate past cash projections were.

Source: PRD-001 §8.14 · Priority S · Phase P2

#### Scenario: Actuals replace forecast months
- **WHEN** a period closes
- **THEN** its actuals replace the forecast for that month automatically

#### Scenario: Weekly projections are kept
- **WHEN** a week's short-term projection (FR-PLN-09) is made
- **THEN** it is kept

#### Scenario: Forecast accuracy
- **WHEN** the forecast accuracy view is opened
- **THEN** it compares each kept weekly projection with actual cash 4 and 13 weeks later
- **AND** it explains the differences by cause (receipts later than expected, unplanned spend, timing)

### Requirement: FR-PLN-05 13-week cash forecast
The system SHALL provide a 13-week cash forecast, the weekly view of the short-term cash flow (FR-PLN-09).

Source: PRD-001 §8.14 · Priority S · Phase P1

#### Scenario: Inputs of the 13-week forecast
- **WHEN** the 13-week cash forecast is produced
- **THEN** it uses open AR and AP (with expected dates from payment behaviour), recurring items, payroll and planned payments

#### Scenario: Same engine in weekly buckets
- **WHEN** the 13-week view is shown
- **THEN** it is the short-term cash flow engine of FR-PLN-09 in weekly buckets

### Requirement: FR-PLN-09 Short-term cash flow
The system SHALL project cash daily for 30, 60, 90 or 180 days (the user picks; default 60), showing the lowest balance and its date against a buffer line.

Source: PRD-001 §8.14 · Priority M · Phase P1

#### Scenario: Horizon
- **WHEN** a user opens the short-term cash flow
- **THEN** the daily projection covers 60 days unless they pick 30, 90 or 180

#### Scenario: Inputs of the projection
- **WHEN** the projection is built
- **THEN** it starts from today's bank balances and adds open invoices and bills on their expected dates (from each contact's payment behaviour), recurring documents, scheduled pay runs, CPF, GST and tax due dates, and planned payments

#### Scenario: Committed contract payments
- **GIVEN** phase P2 and a payment committed in the supplier-contract register (FR-AP-21) that no bill covers yet
- **WHEN** the projection is built
- **THEN** that payment is included

#### Scenario: Lowest balance and buffer alert
- **WHEN** the projection is shown
- **THEN** it shows the lowest balance and its date, and a buffer line
- **AND** an alert is raised when the projection falls below the buffer

#### Scenario: What-ifs are sandboxed
- **WHEN** a user tries a what-if (delay this bill, chase that invoice)
- **THEN** the projection changes in a sandbox
- **AND** nothing posts

#### Scenario: Weekly buckets
- **WHEN** the projection is shown in weekly buckets
- **THEN** it is the 13-week view of FR-PLN-05

### Requirement: FR-PLN-06 Feasibility calculator
The system SHALL provide a feasibility calculator (the workbook model).

Source: PRD-001 §8.14 · Priority S · Phase P1

#### Scenario: Feasibility outputs
- **WHEN** a project's cash flows are entered in the calculator
- **THEN** it shows NPV, IRR, payback, discounted payback, profitability index, break-even, margin of safety, a sensitivity grid and a verdict

### Requirement: FR-PLN-07 Cap table and funding
Forecasts SHALL keep the funding plan, starting from the current cap table; the register, the cap table and round modelling are kept in the equity register (§8.23, FR-EQ-15…27).

Source: PRD-001 §8.14 · Priority S · Phase P2

#### Scenario: Funding plan starts from the current cap table
- **WHEN** a funding plan is set up in a forecast
- **THEN** it starts from the current cap table of the equity register

### Requirement: FR-PLN-08 Forecasts are sandboxed
Forecasts SHALL be sandboxed: they never post to the ledger.

Source: PRD-001 §8.14 · Priority M · Phase P1

#### Scenario: Forecasts never post
- **WHEN** a forecast or scenario is created, changed or run
- **THEN** nothing is posted to the ledger

#### Scenario: AI runs scenarios freely
- **WHEN** the AI runs forecast scenarios
- **THEN** it may do so freely in the read lane

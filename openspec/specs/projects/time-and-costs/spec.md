# projects/time-and-costs Specification

## Purpose
How Fyniq tracks projects: time entry and approval, cost and billing rates, costs on projects, budgets and change orders, billing methods, revenue over time for fixed-fee projects, and project profitability reports that count staff cost once, while keeping payroll-derived rates private. Derived from PRD-001 §8.10 and §J.2.

## Requirements

### Requirement: FR-PRJ-01 Projects
The system SHALL keep projects as a dimension with attributes: customer, budget, dates, status, billing method and project managers.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Project attributes are recorded
- **WHEN** a project is saved with a customer, budget, dates, status, billing method and project managers
- **THEN** each of those values is stored on the project
- **AND** the project is available as a dimension value

#### Scenario: Project managers approve time and see reports
- **GIVEN** a member is a project manager of a project
- **WHEN** time is logged on that project
- **THEN** that member approves the project's time (FR-PRJ-07)
- **AND** sees the project's reports

### Requirement: FR-PRJ-02 Timesheets
The system SHALL record timesheet entries (entry detailed in FR-PRJ-05) with date, person, project, task, hours, billable flag and rate, and SHALL cost each entry at the person's cost rate, using one default formula (FR-PRJ-06).

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Timesheet entry fields
- **WHEN** a timesheet entry is saved
- **THEN** it records the date, person, project, task, hours, billable flag and rate

#### Scenario: Entry costed at the person's cost rate
- **WHEN** a timesheet entry is costed
- **THEN** its cost is its hours at the person's cost rate from the one default formula of FR-PRJ-06

#### Scenario: Workbook hourly cost is a migration difference
- **GIVEN** the G.3 hourly cost figure is the workbook's, without SDL
- **WHEN** migration parity is checked
- **THEN** its difference from Fyniq's cost rate is a migration difference register item

### Requirement: FR-PRJ-05 Time entry
The system SHALL provide time entry through a weekly timesheet grid and a start/stop timer (web; PWA in P2), by project and task, with a billable flag and a note.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Grid and timer
- **WHEN** a person logs time
- **THEN** they can use the weekly timesheet grid or the start/stop timer, choosing the project and task, the billable flag and a note

#### Scenario: Reminders for missing days
- **GIVEN** a person's week has a day with no time entered
- **WHEN** reminders run
- **THEN** the person is reminded of the missing day

#### Scenario: Approved or invoiced entries lock
- **WHEN** a time entry is approved or invoiced
- **THEN** it is locked

#### Scenario: Corrections are reversing entries
- **GIVEN** a locked time entry
- **WHEN** it is corrected
- **THEN** the correction is a reversing entry

### Requirement: FR-PRJ-06 Cost and billing rates
The system SHALL keep an effective-dated cost rate per person and effective-dated billing rates in the project's currency, and SHALL treat a person's payroll-derived cost rate as payroll data (FR-PAY-13, AUD-05).

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Default cost rate
- **WHEN** a person has no overriding cost rate
- **THEN** their cost rate is monthly payroll cost (salary + employer CPF + SDL) ÷ standard hours (the workbook's hourly cost)

#### Scenario: Cost rate override
- **WHEN** a person's cost rate is overridden from an effective date
- **THEN** the override applies from that date

#### Scenario: Who sees a payroll-derived cost rate
- **WHEN** a person's payroll-derived cost rate is requested
- **THEN** only Payroll administrators and Owners see it, unless the Owner grants it to someone else

#### Scenario: Everyone else sees average or standard rates
- **GIVEN** a user without access to payroll-derived rates, project managers included
- **WHEN** they see time at cost
- **THEN** it is at a role or department average rate computed over at least 3 people, or else at an Owner-set standard rate
- **AND** per-person profitability shows hours, not person-level cost

#### Scenario: Billing rates
- **WHEN** billing rates are set
- **THEN** they can be set per project, role, person or customer, effective-dated, in the project's currency

#### Scenario: G4-050 Project manager without payroll rights
- **GIVEN** a project manager without payroll rights
- **WHEN** they open per-person profitability, drill to time entries, call the cost-rate API and ask the AI for a person's cost
- **THEN** no person-level payroll-derived rate or cost is returned in the UI, API, export or AI answer
- **AND** only role-average or standard rates are used

### Requirement: FR-PRJ-07 Timesheet approval
The system SHALL route timesheets to the project manager for approval as an FR-APR-01 object, and SHALL NOT let anyone approve their own time in Standard mode.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Project manager approves
- **WHEN** time on a project is submitted
- **THEN** it goes to the project's manager for approval

#### Scenario: No self-approval in Standard mode
- **GIVEN** the organisation is in Standard mode
- **WHEN** a project manager tries to approve their own time
- **THEN** the approval is refused

### Requirement: FR-PRJ-08 Costs on projects
Bills, claims, mileage, card spend and journals SHALL carry the project dimension, and the system SHALL support billable expenses with an optional markup and purchase orders per project.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Cost documents carry the project
- **WHEN** a bill, claim, mileage claim, card spend line or journal is recorded for a project
- **THEN** it carries the project dimension

#### Scenario: Billable expense with markup
- **WHEN** a project cost is marked billable with a markup
- **THEN** it is billable at its cost plus the markup

#### Scenario: Purchase orders per project
- **WHEN** a purchase order is raised for a project
- **THEN** it is recorded against that project

### Requirement: FR-PRJ-09 Budgets and change orders
The system SHALL keep budgets in hours and money per task, with burn, forecast at completion and alerts at 80% and 100% (settings), and change orders (P2) that change the budget, the fee and the estimate at completion.

Source: PRD-001 §8.10 · Priority S · Phase P1 / P2 (change orders)

#### Scenario: Burn and forecast at completion
- **WHEN** a project's budget is shown
- **THEN** it shows hours and money per task, the burn and the forecast at completion

#### Scenario: Budget alerts
- **WHEN** a task's budget use reaches 80% or 100% (thresholds in settings)
- **THEN** an alert is raised

#### Scenario: Change order
- **GIVEN** an approved change order with the signed document as evidence
- **WHEN** it takes effect from its date
- **THEN** it changes the budget, the fee and, for revenue over time (FR-PRJ-11), the estimate at completion from that date
- **AND** the before and after are both kept

### Requirement: FR-PRJ-10 Billing methods
The system SHALL support the billing methods time and materials, fixed fee by milestone, retainer drawn down, and internal or non-billable projects.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Time and materials
- **GIVEN** a time-and-materials project
- **WHEN** it is invoiced
- **THEN** its approved unbilled time and billable expenses become an invoice
- **AND** the AI may draft the invoice (`ar.draft_invoice`)

#### Scenario: Fixed fee by milestone
- **GIVEN** a fixed-fee project
- **WHEN** it is invoiced
- **THEN** it is invoiced by milestone

#### Scenario: Retainer drawn down
- **WHEN** work is billed on a retainer project
- **THEN** it is drawn down from the retainer

#### Scenario: Internal or non-billable project
- **WHEN** time and costs are recorded on an internal or non-billable project
- **THEN** they are not billed

### Requirement: FR-PRJ-11 Revenue over time for fixed-fee projects
When the accountant elects it, the system SHALL recognise revenue over time for fixed-fee projects by cost-to-cost progress, with a contract asset and contract liability, through a monthly journal proposed for approval.

Source: PRD-001 §8.10 · Priority S · Phase P2

#### Scenario: Monthly journal proposed for approval
- **GIVEN** the accountant has elected revenue over time for a fixed-fee project
- **WHEN** the month ends
- **THEN** a journal recognising revenue by cost-to-cost progress, with a contract asset or contract liability, is proposed for approval

#### Scenario: Costs that do not show progress are excluded
- **WHEN** cost-to-cost progress is measured
- **THEN** costs that do not show progress, such as waste, are excluded (the input method of SFRS(I) 15 B18–B19)

#### Scenario: Basis
- **WHEN** the method is applied
- **THEN** it rests on SFRS(I) 15 ¶35 (over time when, for example, the customer consumes the benefit as the work is done) and B18–B19, and on SFRS for Small Entities (2024 edition) §23.14 and §23.22 (percentage of completion)

#### Scenario: Re-confirmed for the third edition
- **GIVEN** the SFRS for Small Entities third edition applies from periods starting 1 Jan 2027 and moves revenue to a simplified five-step model
- **WHEN** such a period begins
- **THEN** the method is re-confirmed against the third edition's five-step revenue model

### Requirement: FR-PRJ-12 Project reports
The system SHALL report profitability per project, customer and person (revenue − time at cost − direct costs), utilisation, realisation, unbilled work and budget burn, with drill to time entries and costs; time at cost is a management view.

Source: PRD-001 §8.10 · Priority M · Phase P1

#### Scenario: Utilisation and realisation
- **WHEN** the project reports are shown
- **THEN** utilisation is billable ÷ available hours
- **AND** realisation is billed ÷ standard value

#### Scenario: Drill to time entries and costs
- **WHEN** a user drills into a project report figure
- **THEN** the time entries and costs behind it are shown

#### Scenario: Time never posts
- **WHEN** time is logged and approved
- **THEN** nothing posts to the ledger, because salaries are already in the ledger through payroll

#### Scenario: Direct costs exclude payroll-cost accounts
- **WHEN** a project's direct costs are computed
- **THEN** they are its project-tagged ledger costs **excluding** accounts with the payroll-cost flag (FR-COA-02)
- **AND** staff cost enters only as time at cost

#### Scenario: Project-tagged payroll is listed to reconcile
- **GIVEN** salary tagged to a project by a journal or an allocation (FR-DIM-03)
- **WHEN** the project's profitability is shown
- **THEN** the salary is not counted twice
- **AND** the project-tagged payroll lines are listed separately to reconcile

#### Scenario: Available hours
- **WHEN** a person's available hours are computed
- **THEN** they come from the person's effective-dated working pattern (FR-PPL-07), or the organisation's standard hours (effective-dated; default 40 a week, 173.33 a month) when there is none, minus public holidays on that person's working days
- **AND** minus leave hours on working days that are not holidays, so no hour is subtracted twice

#### Scenario: G4-049 Salary tagged to a project with logged time
- **GIVEN** a journal tags S$5,000 of salary to project Acme, and the same people log approved time on Acme that month
- **WHEN** Acme's profitability is shown
- **THEN** Acme's cost counts staff cost once (time at cost)
- **AND** the tagged salary appears on a separate reconciliation line, not in direct costs

#### Scenario: Pattern change, leave across a holiday
- **GIVEN** a part-timer whose working pattern changes mid-month, and a leave interval that spans a public holiday and is later cancelled in part
- **WHEN** available hours are computed
- **THEN** each hour is subtracted at most once
- **AND** the result is reproducible from the recorded pattern and calendar versions, and planned and actual capacity are shown apart

### Requirement: Logging and approving project time
A consultant SHALL be able to log time with the timer and the weekly timesheet grid, submit the week and have the project manager approve it, as in use case UC-PRJ-01 (FR-PRJ-05, FR-PRJ-07).

Source: PRD-001 §J.2

#### Scenario: Timer and grid
- **GIVEN** Priya starts the timer on "Acme rollout · Data migration"
- **WHEN** she stops it after 2 h 15 min
- **THEN** 2 h 15 min is entered on that project and task
- **AND** she fills the rest of the week in the timesheet grid

#### Scenario: Reminder catches a missing day
- **GIVEN** Priya's week has a missing day
- **WHEN** the reminder runs on Friday
- **THEN** it catches the missing day
- **AND** she adds it and submits the week

#### Scenario: Approval locks the entries
- **WHEN** the project manager approves the week
- **THEN** the entries lock

#### Scenario: Correction after approval
- **WHEN** an approved entry is corrected
- **THEN** the correction is a reversing entry plus a new one, re-approved

#### Scenario: Time posts nothing
- **WHEN** the week is approved
- **THEN** nothing is posted to the books; time at cost is a management view (FR-PRJ-12)

### Requirement: Invoicing a time-and-materials project
The AR specialist SHALL be able to invoice a time-and-materials project's approved unbilled time and billable expenses from the Unbilled view, with an AI-drafted invoice, as in use case UC-PRJ-02 (FR-PRJ-06, FR-PRJ-10, `ar.draft_invoice`).

Source: PRD-001 §J.2

#### Scenario: Unbilled view at month end
- **WHEN** at month end the AR specialist opens **Unbilled** for Acme
- **THEN** it shows 62.5 approved hours at the project's billing rates, plus a S$420 billable expense with a 10% markup

#### Scenario: AI drafts, a person posts
- **WHEN** the invoice is prepared
- **THEN** the AI drafts it as a tier-1 proposal
- **AND** the AR specialist reviews and posts it

#### Scenario: Invoiced items lock
- **WHEN** the invoice is posted
- **THEN** the time entries and the expense are marked invoiced and locked

#### Scenario: Unbilled time warning
- **GIVEN** approved time still unbilled 30 days after the period end
- **WHEN** checks run
- **THEN** it shows a warning (⚠) (check C.2 #40)

#### Scenario: Invoice posting
- **WHEN** the invoice posts
- **THEN** it debits 1100 AR and credits Revenue (services) and, if the organisation is GST-registered, 2200 GST control

### Requirement: Fixed-fee project with milestones
A fixed-fee project SHALL be invoiced by accepted milestone and, when the accountant elects revenue over time, recognised monthly by cost-to-cost progress, as in use case UC-PRJ-03 (FR-PRJ-10, FR-PRJ-11).

Source: PRD-001 §J.2

#### Scenario: Each accepted milestone is invoiced
- **GIVEN** a S$30,000 project with three milestones
- **WHEN** a milestone is accepted
- **THEN** that milestone is invoiced

#### Scenario: Revenue over time elected
- **GIVEN** the accountant elects revenue over time for the project
- **WHEN** a month ends
- **THEN** a monthly journal recognising revenue by cost-to-cost progress, with a contract asset or liability for the difference, is proposed for approval (FR-PRJ-11, P2)

### Requirement: Reviewing project budget and profitability
The project page SHALL show the project manager budget use, forecast at completion and margin, with drill-down to the entries behind each figure, as in use case UC-PRJ-04 (FR-PRJ-09, FR-PRJ-12, FR-UX-04).

Source: PRD-001 §J.2

#### Scenario: Budget, forecast and margin on the project page
- **GIVEN** a project has used 340 of 400 budgeted hours
- **WHEN** the project manager opens the project page
- **THEN** it shows 340 of 400 budgeted hours used (85%: ⚠ at 80%), the forecast at completion, and margin = revenue − time at cost − direct costs

#### Scenario: Drill-down behind each figure
- **WHEN** the project manager drills into a figure
- **THEN** the time entries and bills behind it open

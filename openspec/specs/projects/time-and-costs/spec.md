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

#### Scenario: The Friday reminder (rev 0.302)
- **GIVEN** a person on an active project and a Friday in the organisation's time zone
- **WHEN** the daily job runs at 16:00 Singapore time
- **THEN** the person is told once of the weekdays of the week with no time, public holidays excluded, in a notice that is theirs alone, and nobody is told on another day or while Projects and time are off

#### Scenario: Approved or invoiced entries lock
- **WHEN** a time entry is approved or invoiced
- **THEN** it is locked

#### Scenario: Corrections are reversing entries
- **GIVEN** a locked time entry
- **WHEN** it is corrected
- **THEN** the correction is a reversing entry

#### Scenario: Time is whole minutes, one timer, one week at a time
- **WHEN** time is entered
- **THEN** it is whole minutes, 1 to 1,440 an entry, on a task of a project the person is on, and never billable on an internal project
- **AND** the timer runs one at a time and stops into a draft entry of the nearest whole minute, half up, at least one (RND-TIME)
- **AND** a week is Monday to Sunday and is saved and audited as one

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

#### Scenario: Who enters a person-level rate
- **WHEN** a person-level cost rate is entered or read
- **THEN** only the Owner and the Payroll administrator may, through the Appendix D row for project cost rates
- **AND** until payroll supplies one the rate is typed in, and a payroll-derived rate replaces it later

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

#### Scenario: Naming a manager gives no right
- **GIVEN** a member named a manager of a project who holds neither the Approver nor the Controller role
- **WHEN** they try to approve the project's time
- **THEN** it is refused, and the project warns of the manager
- **AND** the Controller approves any project's time and the Approver only the projects they manage, never their own

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

#### Scenario: A billable line needs a project (rev 0.297)
- **WHEN** a bill line or a claim line is billable or has a markup
- **THEN** it carries a project value, a markup is a percentage from 0 to 999.99 with at most two decimals, and a markup belongs to a billable line
- **AND** otherwise it is refused with a plain message naming the line

#### Scenario: A purchase order's project (rev 0.297)
- **GIVEN** a purchase order for a project
- **WHEN** a bill is made from it
- **THEN** each of the bill's lines carries the project as a project value

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

#### Scenario: The unbilled view and the draft invoice (rev 0.298)
- **GIVEN** a time-and-materials project with a customer, approved billable time and posted billable costs
- **WHEN** the unbilled work is drafted into an invoice
- **THEN** time is valued at the billing rate of each day (RND-TIME per entry) and each cost at its cost plus markup (RND-MARKUP)
- **AND** an ordinary draft invoice is made to the customer through the invoice command, each line carrying the project

#### Scenario: Work is billed once (rev 0.298)
- **WHEN** an invoice made from a project's unbilled work is posted
- **THEN** its time entries and cost lines are marked billed by the database, a voided invoice releases them, and no second invoice can be posted for the same work

#### Scenario: Approved time not invoiced (rev 0.298)
- **WHEN** approved billable time on a time-and-materials project is on no posted invoice 30 days after the end of its month
- **THEN** C.2 #40 warns and names the project

#### Scenario: Milestones are billed once after acceptance (rev 0.301)
- **GIVEN** a fixed-fee project with a milestone
- **WHEN** the customer's acceptance is recorded and an invoice is drafted for it
- **THEN** the invoice has one line at the milestone's amount, an unaccepted or already billed milestone is refused, and posting the invoice marks the milestone billed until the invoice is voided

#### Scenario: Retainer drawdown (rev 0.301)
- **GIVEN** a retainer project whose periods are invoiced without acceptance
- **WHEN** its approved billable time is valued at billing rates
- **THEN** the time is drawn down against what the retainer billed, and the balance is billed less drawn, not invoiced as time

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

#### Scenario: The month's figures are a true-up of the revenue the ledger holds (rev 0.571)
- **GIVEN** a fixed fee of 10,000.00, an estimated total cost of 6,000.00, a cost to date of 3,000.00, 4,000.00 invoiced and 6,500.00 of revenue held for the project
- **WHEN** the position is worked out
- **THEN** progress is 50.0%, the revenue earned to date is 5,000.00, the adjustment is minus 1,500.00 and the contract asset is 1,000.00 after the journal
- **AND** a project invoiced ahead of its progress shows a contract liability instead, and the asset's change less the liability's change is the adjustment

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

#### Scenario: Export and metrics (rev 0.331)
- **WHEN** a person exports the project report, or asks the semantic layer for a measure by project
- **THEN** the file or fact holds only what the person's own report holds, hours and revenue for a viewer who may see no costs
- **AND** the semantic layer gives approved minutes by project and never a rate or a person's cost

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

#### Scenario: Lines tagged with no project (rev 0.296)
- **WHEN** the project report is shown to someone who sees every project
- **THEN** a row for lines tagged with no project is shown, so every project plus that row equals the revenue and expense accounts' totals

#### Scenario: Cost by tier in the report (rev 0.296)
- **WHEN** the report shows time at cost
- **THEN** a person's own rate is used only for the Owner and the Payroll administrator
- **AND** others see the role average over at least three people or the standard rate, and the AR specialist sees revenue and hours only

#### Scenario: Project checks (rev 0.296)
- **GIVEN** a project over its hours budget, a week submitted or returned more than 7 days ago, or a closed week with no approved time
- **WHEN** the checks run
- **THEN** C.1 #26, C.1 #27 and C.2 #41 each warn, name the project or week, and say how to fix it

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

# experience/navigation Specification

## Purpose
How people move through Fyniq and work in it: the sidebar and its items, the AI side panel, record pages, the command palette, search, Inbox triage, drill-down, keyboard-first entry, grids, status chips, guided set-up, safety, notifications, live updates and clipboard fidelity, and the usability tests the experience must pass. Derived from PRD-001 §7.1, PRD-001 §7.2, PRD-001 §7.3 and PRD-001 §7.7.

## Requirements

### Requirement: Sidebar navigation
The top-level navigation SHALL be a left sidebar of at most 13 items, grouped by job rather than accounting jargon, with Settings in its footer.

Source: PRD-001 §7.1

#### Scenario: Collapsed sidebar
- **WHEN** the user collapses the sidebar
- **THEN** it shows icons only
- **AND** each icon's label appears in a tooltip

#### Scenario: At most 13 items
- **WHEN** the sidebar is shown in Expert mode with every feature switched on
- **THEN** it has at most 13 top-level items, not counting the personal section and Settings in the footer

#### Scenario: Feature-switched items
- **WHEN** a feature is switched off for the organisation (FR-ORG-06)
- **THEN** its navigation item (Projects, Equity, Payroll) is hidden

#### Scenario: Items without a right
- **WHEN** a person has no right in an area
- **THEN** its navigation item is not shown

#### Scenario: Personal rights only
- **WHEN** a person's Fyniq rights are personal only
- **THEN** they get the personal workspace instead of the sidebar (FR-UX-39)

### Requirement: Navigation items and badges
Each top-level navigation item SHALL contain the pages of its job and SHALL show its badge where it has one.

Source: PRD-001 §7.1

#### Scenario: Home
- **WHEN** the user opens Home
- **THEN** it shows a role-aware "Today": cash, runway, books health, to-dos, upcoming deadlines and AI suggestions, with no badge

#### Scenario: Inbox
- **WHEN** the user opens Inbox
- **THEN** it shows everything waiting for them: approvals, AI proposals, items that cannot post (✖), items to review (⚠), mentions and compliance tasks, and the **AI tasks** page (catalogue and proposal history, §9.6)
- **AND** its badge is the count of waiting items

#### Scenario: Sales
- **WHEN** the user opens Sales
- **THEN** it holds quotes and orders, invoices, credit notes, receipts, statements, recurring invoices and items
- **AND** its badge is the overdue count

#### Scenario: Purchases
- **WHEN** the user opens Purchases
- **THEN** it holds purchase orders, bills (with the bills board and calendar, FR-AP-13), vendor credits, payment batches and expense claims (all users' claims, for approvers and AP)
- **AND** its badge is the due count

#### Scenario: Contacts
- **WHEN** the user opens Contacts
- **THEN** customers and vendors are one list, filterable by role, with balances, activity and bank-detail changes, and no badge

#### Scenario: Banking
- **WHEN** the user opens Banking
- **THEN** it holds bank and card accounts, feeds and imports, categorise, match, rules, transfers and reconciliations
- **AND** its badge is the uncategorised count

#### Scenario: Payroll
- **WHEN** the user opens Payroll
- **THEN** it holds the pay runs received from Nemura or imported, their postings and salary payment batches, with links to Nemura, plus the people projection (department, cost centre, line manager, dates, holds)
- **AND** its badge is the due status
- **AND** employees, payslips and IR8A live in Nemura or the payroll provider's system, never in Fyniq (FR-PPL-01, PRD-002 NM-PAY-07)

#### Scenario: Projects
- **GIVEN** projects are switched on
- **WHEN** the user opens Projects
- **THEN** it holds projects, timesheets and profitability
- **AND** its badge is the over-budget count

#### Scenario: Accounting
- **WHEN** the user opens Accounting
- **THEN** it holds journals, chart of accounts, dimensions, fixed assets, accruals and schedules, FX, period close, year end, and director accounts and share issues (§8.22)
- **AND** its badge is the close status

#### Scenario: Reports
- **WHEN** the user opens Reports
- **THEN** it holds financial statements, management reports, tax reports, saved and scheduled reports and the board pack, with no badge

#### Scenario: Planning
- **WHEN** the user opens Planning
- **THEN** it holds budgets, forecasts and scenarios, 13-week cash, feasibility and the funding plan, with no badge
- **AND** the cap table is not in Planning but in Equity

#### Scenario: Equity
- **GIVEN** equity is switched on
- **WHEN** the user opens Equity
- **THEN** it shows who owns the company (cap table, issued and fully diluted), shareholders and investors, share classes, share issues and transfers, SAFEs and notes, option pool, round simulator and investor updates (§8.23)
- **AND** its badge is the count of ACRA tasks due

#### Scenario: Compliance
- **WHEN** the user opens Compliance
- **THEN** it holds the statutory calendar, GST returns, filings, the Compliance Center (controls and evidence), the audit trail explorer and the **AI company secretary** (runs, findings, drafts, schedule; §9.12)
- **AND** its badge is the due count

#### Scenario: Settings
- **WHEN** the user opens Settings from the sidebar footer
- **THEN** it holds organisation, users and roles, team mode, approval policies, AI settings and task catalogue, **connected apps** (API and MCP clients, §9.7), integrations and webhooks, statutory parameters, and appearance and motion (FR-UX-38), with no badge

### Requirement: Personal section
Every user SHALL see a personal section at the top of the sidebar, not counted in the 13-item cap.

Source: PRD-001 §7.1

#### Scenario: Personal items
- **WHEN** any user opens the sidebar
- **THEN** the personal section shows **My claims** (FR-AP-14)
- **AND** directors also see "Money I put in" (FR-EQ-14)
- **AND** it shows My time when Projects is on (FR-PRJ-05) and My shares and options for a person with grants (FR-EQ-19)
- **AND** it never shows payslips: they are in Nemura's My pay (PRD-002 NM-ESS-02) or the payroll provider's system

#### Scenario: The whole navigation for personal-only users
- **GIVEN** a person whose Fyniq rights are personal only
- **WHEN** they open Fyniq
- **THEN** the personal section is the whole navigation (FR-UX-39)

#### Scenario: What each role sees
- **WHEN** a role in §7.1's table signs in
- **THEN** its landing page, navigation and Home match that table (QA-UI-08)
- **AND** a person with several roles sees the union

### Requirement: Workspace by role
Each role SHALL land, navigate and see Home as §7.1's table says, derived from Appendix D: an area only where the role holds a right, approve-only rights as Inbox items, and the union for a person with several roles.

Source: PRD-001 §7.1

#### Scenario: Employee
- **WHEN** a person holding only the Employee role signs in
- **THEN** they land on My claims in the personal workspace, with My time, My shares and options, Money I put in (directors) and Help
- **AND** no company figure and no other person's record is shown

#### Scenario: Line manager
- **WHEN** a line manager holding only the Employee role signs in
- **THEN** they see the Employee's workspace plus To approve with their reports' claims
- **AND** a report's claim is visible only while it waits for them

#### Scenario: Budget holder or project manager
- **WHEN** an Approver for a department or projects signs in
- **THEN** they land on My department, with Home, Inbox, their projects, their department's reports and budget, and the personal items
- **AND** company cash, runway and bank, other departments, Sales, Purchases, Contacts, bank details, Payroll, Accounting, Equity, Compliance and Settings are not shown
- **AND** payroll cost appears only as a department total covering at least 3 people besides the viewer

#### Scenario: Bookkeeper
- **WHEN** a Bookkeeper signs in
- **THEN** Home shows books health, bank lines to categorise, documents to finish, deadlines and cash today
- **AND** the navigation holds Home, Inbox, Sales, Purchases, Contacts, Banking, Accounting (view) and Reports, without Payroll, Projects, Planning, Equity, the Compliance Center or Settings

#### Scenario: AR specialist
- **WHEN** an AR specialist signs in
- **THEN** Home shows overdue invoices, receipts to apply and quotes waiting
- **AND** the navigation holds Home, Inbox, Sales, customers, Banking (view), Projects (revenue and hours) and sales reports, without Purchases, Payroll, Accounting, Planning, Equity or Compliance

#### Scenario: AP specialist
- **WHEN** an AP specialist signs in
- **THEN** Home shows bills due, batches to prepare, claims to pay and bank-detail changes waiting
- **AND** the navigation holds Home, Inbox, Purchases, vendors, Banking (view) and purchase reports, without Sales, Payroll, Accounting, Planning, Equity or Compliance

#### Scenario: Payment releaser
- **WHEN** a Payment releaser signs in
- **THEN** Home shows batches waiting for release, bank balances and payments rejected or with an unknown outcome
- **AND** salary lines appear only as totals, and nothing lets them create or approve a payment

#### Scenario: Payroll administrator
- **WHEN** a Payroll administrator signs in
- **THEN** Home shows the next pay run, salary batches, payroll postings and CPF due, with the Finance | People switch when Nemura is linked
- **AND** Sales, Purchases beyond salary batches, Banking detail, Accounting and Equity are not shown

#### Scenario: Accountant and tax agent
- **WHEN** an Accountant or a Tax agent signs in
- **THEN** they get the full Home and every finance area, with Payroll masked and Compliance as view; a Tax agent also gets the GST and tax working papers
- **AND** Settings administration, user administration and per-person pay unless granted are not shown

#### Scenario: Controller
- **WHEN** a Controller signs in
- **THEN** they get the full Home, every finance area and the approval policies and statutory parameters in Settings, without per-person pay unless granted

#### Scenario: Owner
- **WHEN** an Owner signs in
- **THEN** everything is shown, and their actions stay under dual control (§6.4)

#### Scenario: Administrator
- **WHEN** an Administrator signs in
- **THEN** they land on Settings, with role and integration changes in the Inbox, Reports and Compliance (view)
- **AND** nothing lets them post or approve a transaction

#### Scenario: Auditor
- **WHEN** an Auditor signs in
- **THEN** they land in the time-boxed, read-only Auditor workspace with the engagement's requests, samples and expiry
- **AND** payroll personal data is hidden unless granted

#### Scenario: Board viewer
- **WHEN** a Board viewer signs in
- **THEN** they land on the board pack, with Equity showing their own holding, or all holdings if granted, and no transactions

#### Scenario: Company secretary
- **WHEN** a Company secretary signs in
- **THEN** they land on Compliance with statutory deadlines and register updates, and see no transactions, payroll or bank details

#### Scenario: Data protection officer
- **WHEN** the DPO signs in
- **THEN** they land on the Compliance Center with data requests due, breaches and retention holds, and Settings as view
- **AND** transactions are not shown and payroll is masked

#### Scenario: HR officer with Nemura only
- **WHEN** an HR officer who holds no Fyniq role signs in
- **THEN** they land on Nemura's People home, and Fyniq is not offered

#### Scenario: Several roles
- **GIVEN** a person who is both a Bookkeeper and a line manager
- **WHEN** they sign in
- **THEN** they see the union: the Bookkeeper's finance workspace, with their reports' claims waiting in the Inbox

### Requirement: AI assistant side panel
The AI assistant SHALL be a persistent side panel available on every page and carrying the page's context, not a navigation item.

Source: PRD-001 §7.1

#### Scenario: Opening the AI panel
- **WHEN** the user presses ⌘J / Ctrl+J or chooses "Ask about this" (FR-UX-17) on any page
- **THEN** the AI side panel opens with the page's context

#### Scenario: Not a navigation item
- **WHEN** the sidebar is shown
- **THEN** the AI assistant is not one of its items

### Requirement: Expert and Basic navigation lists
The full list of navigation items SHALL be the Expert mode list, and Basic mode (§7.8) SHALL show a shorter list of the essentials, with one toggle in the header to switch.

Source: PRD-001 §7.1

#### Scenario: Basic mode navigation
- **GIVEN** a user in Basic mode
- **WHEN** the sidebar is shown
- **THEN** it shows the shorter list of essentials

#### Scenario: Switching from the header
- **WHEN** the user uses the mode toggle in the header
- **THEN** the sidebar switches between the Basic and Expert lists

### Requirement: Record page anatomy
Every record page (invoice, bill, transaction, journal, pay run, asset, contact) SHALL have the same anatomy: a header, body tabs and a collapsible right panel.

Source: PRD-001 §7.3

#### Scenario: Header
- **WHEN** a record page opens
- **THEN** its header shows the number, status chip, contact, key amounts and primary actions (Approve, Post, Send, Record payment)
- **AND** a "⋯" menu holds the other actions

#### Scenario: Body tabs
- **WHEN** a record page opens
- **THEN** its body has the tabs Details, Lines, Ledger impact, Attachments, Activity and Related

#### Scenario: Ledger impact tab
- **WHEN** the Ledger impact tab is shown
- **THEN** it lists the debits and credits the record posts
- **AND** it is always visible to accountants and optional for others

#### Scenario: Activity and Related tabs
- **WHEN** the Activity and Related tabs are opened
- **THEN** Activity shows the history, approvals and comments from the audit trail
- **AND** Related shows payments, credit notes and the source quote

#### Scenario: Right panel
- **WHEN** a record page opens
- **THEN** a collapsible right panel shows the checks for this record, AI suggestions and context

### Requirement: Usability acceptance
Fyniq SHALL pass the usability acceptance tests UX-T1 to UX-T7 at their targets.

Source: PRD-001 §7.7

#### Scenario: UX-T1 Founder records and pays a supplier bill
- **GIVEN** a founder with no accounting training, including at least one founder who did not take part in building Fyniq
- **WHEN** they record a supplier bill and pay it
- **THEN** they finish in ≤ 5 minutes with no help needed

#### Scenario: UX-T2 Why cash changed
- **WHEN** a user looks for why "Cash" on the dashboard changed since last week
- **THEN** they reach the transactions in ≤ 3 clicks

#### Scenario: UX-T3 Keyboard-only approvals from the Inbox
- **WHEN** a user approves three routine items (tier-1 proposals, or documents within the self-approval limit) from the Inbox using only the keyboard, each through its approval confirmation (FR-UX-03)
- **THEN** it is possible and takes ≤ 30 seconds
- **AND** tier-2 proposals, FM-1 actions and step-up approvals also work from the keyboard, with no time target

#### Scenario: UX-T4 First-time set-up from the workbook
- **WHEN** a user does the first-time set-up from the workbook export
- **THEN** it takes ≤ 60 minutes including import

#### Scenario: UX-T5 System Usability Scale
- **WHEN** the System Usability Scale is measured with the founders, the accountant and at least three people who did not build or specify Fyniq (for example founders of other start-ups)
- **THEN** the score is ≥ 80
- **AND** it is reported per group

#### Scenario: UX-T6 Basic mode bill without accounting terms
- **GIVEN** a founder in **Basic** mode
- **WHEN** they complete UX-T1, switching to **Expert** halfway
- **THEN** they meet no debit, credit or account code in Basic mode
- **AND** the switch keeps the draft and every entered value, with 0 values lost

#### Scenario: UX-T7 Expert feature found from Basic mode
- **GIVEN** a user in Basic mode
- **WHEN** they look for an Expert-only feature (for example "journal") from ⌘K
- **THEN** they reach it in ≤ 2 actions

### Requirement: FR-UX-01 Command palette
The system SHALL provide a command palette (⌘K / Ctrl+K) to go to any page or record, create anything, run a report, change theme, or hand the query to the AI assistant.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Opening the palette
- **WHEN** the user presses ⌘K or Ctrl+K
- **THEN** the command palette opens

#### Scenario: Go to, create, report and theme
- **WHEN** the user types in the palette
- **THEN** they can go to any page or record, create anything (for example "new invoice for Acme"), run a report or change theme

#### Scenario: Hand the query to the AI assistant
- **WHEN** the user presses Tab in the palette
- **THEN** the query is handed to the AI assistant

#### Scenario: Fuzzy search, recent items first
- **WHEN** the user types a partial or misspelt name
- **THEN** fuzzy matches are listed
- **AND** recent items come first

### Requirement: FR-UX-02 Global search
The system SHALL provide global search across documents, contacts, accounts, amounts, references and notes, with type filters and keyboard navigation.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Search by amount
- **WHEN** the user searches for "1,234.56"
- **THEN** records with that amount are found
- **AND** a search for "> 5k" finds records with amounts above 5,000

#### Scenario: Type filters and keyboard navigation
- **WHEN** search results are shown
- **THEN** they can be filtered by type
- **AND** they can be navigated with the keyboard

#### Scenario: References and notes
- **WHEN** the user searches for text in a reference or a note
- **THEN** the records with that reference or note are found

### Requirement: FR-UX-03 Inbox triage
The Inbox SHALL support triage with keyboard shortcuts, bulk actions where safe and grouping by type and due date, and its approve shortcut SHALL open the approval confirmation and never approve directly.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Triage shortcuts
- **WHEN** the user works in the Inbox
- **THEN** J and K move between items, A approves, R rejects, E opens and ? shows help

#### Scenario: A opens the approval confirmation
- **WHEN** the user presses A on an item
- **THEN** the approval confirmation opens, with step-up where FR-APR-04 requires it
- **AND** nothing is approved until the user confirms

#### Scenario: Grouping
- **WHEN** the Inbox is shown
- **THEN** items can be grouped by type and by due date

#### Scenario: No bulk approval of high-risk items
- **WHEN** the user selects several items for a bulk action
- **THEN** bulk actions are offered only where safe
- **AND** tier-2 and tier-3 AI proposals and FM-1 actions cannot be bulk-approved

### Requirement: FR-UX-04 Drill-down everywhere
Any figure in any report or tile SHALL open its breakdown, then its ledger lines, then the source document, then its history (the "Trace" panel).

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Drill from a figure to its history
- **WHEN** the user opens a figure in a report or tile
- **THEN** they can go from its breakdown to its ledger lines, to the source document and to its history in the "Trace" panel

#### Scenario: Breadcrumbs keep the report context
- **WHEN** the user drills down from a report
- **THEN** breadcrumbs keep the report context

#### Scenario: Back returns to the same place
- **WHEN** the user goes Back after drilling down
- **THEN** the report returns to the same scroll position and filters

### Requirement: FR-UX-05 Side-peek panels
The system SHALL open a record in a side-peek panel over the current list without losing the user's place, and SHALL offer a split view for matching.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Side-peek over a list
- **WHEN** the user opens a record from a list in a side-peek panel
- **THEN** the record shows over the list
- **AND** closing it returns to the same place in the list

#### Scenario: Split view for matching
- **WHEN** the user matches a bank line to an invoice
- **THEN** the bank line and the invoice are shown side by side in a split view

### Requirement: FR-UX-06 Keyboard-first entry
The system SHALL support keyboard-first entry: shortcuts for every create action, "G then" navigation keys, Enter-to-next-field, bulk actions on selected rows from the command palette and a shortcut sheet on `?`.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Create and navigation shortcuts
- **WHEN** the user types `C I` or `G B`
- **THEN** `C I` starts a new invoice and `G B` goes to Banking

#### Scenario: Enter moves to the next field
- **WHEN** the user presses Enter in a form field
- **THEN** focus moves to the next field

#### Scenario: Bulk actions and shortcut sheet
- **WHEN** the user selects rows and opens the command palette, or presses `?`
- **THEN** the palette offers bulk actions on the selected rows, and `?` shows the shortcut sheet

#### Scenario: Single-character shortcuts can be turned off and remapped
- **WHEN** a user turns off or remaps single-character shortcuts
- **THEN** the change applies to that user only (WCAG 2.1 SC 2.1.4, Level A)

#### Scenario: No shortcuts while typing
- **GIVEN** focus is in a text field
- **WHEN** the user types a single-character shortcut key
- **THEN** the shortcut does not fire

### Requirement: FR-UX-07 Spreadsheet-like grids
The system SHALL provide spreadsheet-like grids for bulk work (journal lines, bank categorisation, budgets, opening balances, imports), virtualised to 100,000 rows.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Paste and edit like a spreadsheet
- **WHEN** the user works in a grid
- **THEN** they can paste from Excel or Google Sheets, fill down, edit several cells at once, and undo and redo

#### Scenario: Arithmetic in amount cells
- **WHEN** the user types `=` followed by arithmetic in an amount cell
- **THEN** the cell takes the result

#### Scenario: Inline validation
- **WHEN** a row in a grid is invalid
- **THEN** it is validated inline with a status chip

#### Scenario: 100,000 rows
- **WHEN** a grid holds 100,000 rows
- **THEN** it is virtualised and stays usable

### Requirement: FR-UX-08 Plain view and Accountant view
Each user SHALL be able to toggle between Plain view ("Money in / Money out / Category") and Accountant view ("Debit / Credit / Account"), both showing the same data.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Same data in both views
- **WHEN** a user toggles between Plain view and Accountant view
- **THEN** the columns change between "Money in / Money out / Category" and "Debit / Credit / Account"
- **AND** the data shown is the same

#### Scenario: Only in Expert mode
- **GIVEN** a user in Basic mode
- **WHEN** a record or list is shown
- **THEN** it uses Plain view and the toggle is not offered (§7.8)

### Requirement: FR-UX-09 Status chips
The system SHALL show statuses as chips in the workbook vocabulary, each an SVG icon from the single icon set (§7.4) plus a text label, with a tooltip that says what is wrong and how to fix it, and colour SHALL never carry meaning alone.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Chip vocabulary
- **WHEN** a status is shown
- **THEN** it is one of ✔ OK, ⚠ included, please review, ✖ cannot post until fixed, ⏰ due or overdue, ✎ draft, 🔒 locked period, or ⚠ Not assessed

#### Scenario: Not assessed
- **WHEN** a required input is missing or out of date (the "unknown" state of §5.2)
- **THEN** the chip is ⚠ **Not assessed**
- **AND** its tooltip names the input and "Fix it" opens the setting

#### Scenario: Tooltip and Fix it
- **WHEN** the user hovers over or focuses a chip
- **THEN** its tooltip says what is wrong and how to fix it
- **AND** a "Fix it" action is offered where possible

#### Scenario: Icon plus text in the UI
- **WHEN** a chip is shown in the UI
- **THEN** it is an SVG icon from the single icon set (§7.4) plus a text label, and colour is never the only cue

#### Scenario: Workbook symbols outside the UI
- **WHEN** statuses are written to CSV, plain-text email or exports
- **THEN** the workbook symbols are used

### Requirement: FR-UX-10 Guided set-up
The system SHALL guide set-up with a checklist that shows progress, and SHALL offer a sample company sandbox to explore safely.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Set-up checklist
- **WHEN** a new organisation is set up
- **THEN** a checklist covers the company, financial year, GST, bank accounts, opening balances, contacts, employees and import from the workbook
- **AND** it shows progress

#### Scenario: Sample company sandbox
- **WHEN** a user opens the sample company
- **THEN** they can explore it without touching the real books

### Requirement: FR-UX-11 Teaching empty states and contextual help
Every empty list SHALL explain what goes there and offer the first action; glossary terms SHALL have hover definitions; and a "What happened → where to record it" helper SHALL be offered.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Empty list
- **WHEN** a list is empty
- **THEN** it explains what goes there
- **AND** it offers the first action

#### Scenario: Glossary terms
- **WHEN** the user hovers over a glossary term
- **THEN** its definition is shown

#### Scenario: What happened, where to record it
- **WHEN** the user opens the "What happened → where to record it" helper (from the workbook's Start Here)
- **THEN** it leads them from what happened to where to record it

### Requirement: FR-UX-12 Favourites, recent items and saved views
Each user SHALL have favourites, recent items and saved views (filters, columns, sort), and views SHALL be shareable by link.

Source: PRD-001 §7.2 · Priority S · Phase P1

#### Scenario: Saved view
- **WHEN** a user saves a view
- **THEN** its filters, columns and sort are stored for that user

#### Scenario: Favourites and recent items
- **WHEN** a user marks a favourite or opens a record
- **THEN** it appears in their favourites or recent items

#### Scenario: Shared view link
- **WHEN** a user shares a view link and another user opens it
- **THEN** the same view opens

### Requirement: FR-UX-13 Comments and mentions
The system SHALL allow comments and @mentions on any record, with notifications.

Source: PRD-001 §7.2 · Priority S · Phase P1

#### Scenario: Mention notifies
- **WHEN** a user comments on a record and @mentions a colleague
- **THEN** the comment is kept on the record
- **AND** the colleague is notified

### Requirement: FR-UX-14 Undo-friendly safety
Nothing SHALL be hard-deleted by any user action; only the audited retention purge of §11.9 removes data.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: No hard delete
- **WHEN** a user removes a record
- **THEN** it is not hard-deleted

#### Scenario: Void with reason
- **WHEN** a user voids a document
- **THEN** they must give a reason

#### Scenario: Consequences and confirmation
- **WHEN** a user starts a destructive or high-risk action
- **THEN** its consequences are shown and confirmation is required
- **AND** a bulk void requires typed confirmation

### Requirement: FR-UX-15 Notifications
The system SHALL notify users in-app, by email and in a daily digest, with Slack or similar in P2, and with user-level preferences (detailed in FR-NTF-01…06).

Source: PRD-001 §7.2 · Priority S · Phase P1 / P2

#### Scenario: Notification channels
- **WHEN** something needs a user's attention
- **THEN** they are notified in-app and by email, or in their daily digest, according to their preferences

#### Scenario: Slack or similar
- **GIVEN** P2
- **WHEN** a user chooses Slack or a similar messaging channel
- **THEN** notifications are also sent there

### Requirement: FR-UX-16 Responsive PWA and offline receipt queue
The system SHALL offer an installable PWA for approvals, receipt capture (camera), claims and dashboards, with an offline receipt queue. Payslips are not in Fyniq: they are in Nemura's My pay or the payroll provider's system.

Source: PRD-001 §7.2 · Priority S · Phase P2

#### Scenario: Camera capture in the PWA
- **WHEN** a user installs the PWA and captures a receipt with the camera
- **THEN** the receipt is captured in the PWA

#### Scenario: Offline queue is encrypted and temporary
- **WHEN** receipts are queued offline
- **THEN** they are stored encrypted on the device for a limited time (setting)
- **AND** they are removed at sign-out

#### Scenario: Upload after a rights check
- **WHEN** queued receipts are uploaded
- **THEN** the server re-checks the user's rights first
- **AND** duplicates are removed by a client-generated ID and the file hash

#### Scenario: No queue on shared devices
- **GIVEN** a device marked as shared
- **WHEN** the user is offline
- **THEN** the offline queue is off

#### Scenario: Scope of this requirement
- **WHEN** responsive layouts are needed for a screen
- **THEN** they follow FR-UX-37 (P1); this requirement covers the installable PWA, camera capture and the offline queue

#### Scenario: G4-100 Offline queue on a shared device
- **GIVEN** (P2) receipts are queued offline on a shared device
- **WHEN** the user signs out, and another user's role is removed before sync
- **THEN** the local copies are removed at sign-out
- **AND** a sync after the role removal is refused

### Requirement: FR-UX-17 Contextual AI
An "Ask about this" action on any record, report or figure SHALL open the AI assistant with that context attached (§9).

Source: PRD-001 §7.2 · Priority S · Phase P1

#### Scenario: Ask about a figure
- **WHEN** the user chooses "Ask about this" on a record, report or figure
- **THEN** the AI assistant opens with that record, report or figure attached as context

### Requirement: FR-UX-18 Multi-entity switcher
The header SHALL have a multi-entity switcher, and "all entities" views SHALL be offered where the user has access.

Source: PRD-001 §7.2 · Priority C · Phase P3

#### Scenario: Switching entity
- **WHEN** the user picks an entity in the header switcher
- **THEN** the pages show that entity

#### Scenario: All entities view
- **GIVEN** a user with access to every entity
- **WHEN** they choose "all entities"
- **THEN** the view covers all entities

### Requirement: FR-UX-19 Live updates
Open pages SHALL be notified of changes to the records and lists they show through a change feed, and conflicting edits SHALL be caught by ETag / `If-Match` (FR-INT-09).

Source: PRD-001 §7.2 · Priority S · Phase P1

#### Scenario: Change notification and re-read
- **WHEN** a record shown on an open page changes
- **THEN** the page receives an event carrying only the record type, ID and version
- **AND** it re-reads through the normal API, so permissions and AI exclusions apply as on any read (SEC-34)

#### Scenario: Conflicting edit
- **WHEN** a user saves an edit to a record someone else changed
- **THEN** the conflict is caught by ETag / `If-Match` and shown as "changed by X — review and re-apply"
- **AND** no CRDT is used in P1

#### Scenario: Transport
- **WHEN** the transport for live updates is chosen
- **THEN** it is server-sent events or polling, chosen by a P0 measurement under NFR-COST-02

#### Scenario: Stream closes
- **WHEN** the tab has been hidden for 5 minutes [proposed], the session reaches its idle timeout (SEC-05) or the Cloud Run request timeout is reached
- **THEN** the stream closes

#### Scenario: Resume or reload
- **WHEN** a page reconnects
- **THEN** it resumes from the last event ID through the change feed's resume cursor
- **AND** if the cursor is older than the feed keeps, the page reloads

#### Scenario: Events through the database
- **WHEN** an event is published
- **THEN** it reaches every API instance through the database, with no extra service

#### Scenario: G4-077 Missed events and a restricted record
- **GIVEN** a page misses events (laptop asleep past the feed window) and a restricted record changed meanwhile
- **WHEN** the page reconnects
- **THEN** it reloads its state once with no duplicated rows
- **AND** the restricted record is not shown to a user without the right

### Requirement: FR-UX-20 Clipboard and paste fidelity
Copied amounts and exports SHALL use an ASCII minus and plain decimals, and pasted amounts SHALL be parsed in common formats with the parsed value previewed before commit.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Copy and export
- **GIVEN** an amount displayed as "−1,234.56"
- **WHEN** it is copied or exported
- **THEN** it is written as "-1234.56": an ASCII minus, no thousands separators, a dot as the decimal mark and no currency symbol, the typographic "−" being display-only

#### Scenario: Paste formats
- **WHEN** the user pastes `(1,234.56)`, `S$1,234.56`, `−1,234.56` or an amount in their locale format
- **THEN** the amount is parsed
- **AND** the parsed value is previewed before commit

### Requirement: FR-UX-21 Accessible live counts
Badge and status changes SHALL be announced through one polite, atomic status region as contextual phrases, never as bare numbers, and SHALL never move focus.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Count change is announced
- **WHEN** the Inbox badge changes to 3 approvals
- **THEN** the status region announces "3 approvals waiting", not "3"
- **AND** focus stays where it was

### Requirement: FR-UX-22 No optimistic money
Figures derived from the ledger SHALL render only from server responses; optimistic updates SHALL be allowed only for non-financial UI state (comments, favourites, layout).

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Ledger figure after a change
- **WHEN** a user saves a change that affects a ledger figure
- **THEN** the figure updates only when the server response arrives

#### Scenario: Optimistic UI state
- **WHEN** a user adds a comment, marks a favourite or changes the layout
- **THEN** the UI may update before the server responds

### Requirement: FR-UX-39 Personal workspace for people who only claim
A person whose Fyniq rights are personal only SHALL get a separate workspace that lands on My claims, shows only their own items (claims, time, shares and options, money put in) and, for a line manager, To approve, and SHALL keep search, notifications and the assistant to their own records, the policy and help.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Landing
- **GIVEN** Daniel Lim holds only the Employee role in Fyniq
- **WHEN** he signs in to Fyniq, or arrives from Nemura's Claims tile
- **THEN** he lands on My claims, with no Home, Inbox, Basic | Expert switch or finance area in the navigation

#### Scenario: A finance link
- **WHEN** he opens a link to a finance page, for example a bank account
- **THEN** he sees a plain "not part of your access" page with a way back, and no names or figures

#### Scenario: Search and the assistant
- **WHEN** he searches or asks the assistant about money
- **THEN** only his own claims, the expense policy and help are reached (SEC-34, AI-P1)

#### Scenario: Line manager
- **GIVEN** a line manager who approves their reports' claims and holds only the Employee role in Fyniq
- **WHEN** a claim waits for them
- **THEN** it appears under To approve and opens its approval confirmation with the receipt, never the Purchases module

#### Scenario: Delegated approver
- **GIVEN** a budget holder with the Approver role for their department
- **WHEN** they sign in
- **THEN** they get the finance workspace cut to their rights (FR-UX-40), not the personal workspace

#### Scenario: Own time and grants
- **GIVEN** Projects is on and the employee holds option grants
- **WHEN** they open the personal workspace
- **THEN** My time (FR-PRJ-05) and My shares and options (FR-EQ-19) are there too

#### Scenario: Enforced on the server
- **WHEN** a personal-only user calls a finance API operation directly
- **THEN** it is refused by permissions and row-level security, whatever the navigation shows

#### Scenario: Back to People
- **GIVEN** he arrived from Nemura
- **WHEN** the personal workspace is shown
- **THEN** the header has Back to People instead of the Finance | People switch

#### Scenario: Rights change
- **WHEN** he is given a finance role
- **THEN** his next page load shows the full workspace, and removing the role moves him back

### Requirement: FR-UX-40 Every workspace is cut to the person's rights
Each navigation area SHALL appear only where the person holds a right, approve-only rights SHALL arrive as Inbox items, Home SHALL follow rights, search, exports and the AI SHALL keep the same scope, and the server SHALL enforce every right whatever is shown.

Source: PRD-001 §7.2 · Priority M · Phase P1

#### Scenario: Budget holder's Home
- **GIVEN** Wei Jie Ong holds the Approver role for Engineering
- **WHEN** he signs in
- **THEN** Home is My department: Engineering's budget against actual, its spending, approvals waiting with his limit
- **AND** no company cash, runway, bank balance or other department appears

#### Scenario: Approve-only rights
- **GIVEN** a budget holder who may approve bills within their department and limit
- **WHEN** a bill waits for them
- **THEN** it arrives in their Inbox, and the Purchases area is not in their navigation

#### Scenario: Department payroll cost
- **WHEN** a budget holder's department payroll cost covers fewer than 3 people besides them
- **THEN** it is not shown as a figure (FR-PAY-13)

#### Scenario: Same scope everywhere
- **WHEN** a budget holder searches, exports or asks the assistant about spending
- **THEN** only their department's and projects' figures are reached (SEC-34, AI-P1)

#### Scenario: Rights change
- **WHEN** a person gains or loses a right
- **THEN** what they see changes at their next page load, and the server enforces the right either way

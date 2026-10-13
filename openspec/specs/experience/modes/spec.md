# experience/modes Specification

## Purpose
How Fyniq offers two experience modes, Basic with only the essentials and Expert with every lever, switched per user without changing permissions, validation, posting or the data saved, and how each area of the product differs between them. Derived from PRD-001 §7.8.

## Requirements

### Requirement: Basic and Expert navigation
Basic mode SHALL show the essential navigation items (≤ 10) and Expert mode SHALL add Accounting, Planning, Compliance and all sub-pages (≤ 13 items, §7.1); what each mode shows is presentation only.

Source: PRD-001 §7.8

#### Scenario: Basic navigation
- **GIVEN** a user in Basic mode
- **WHEN** the navigation is shown
- **THEN** it shows Home, Inbox, Sales, Purchases, Contacts, Banking and Reports; Payroll and Projects when the role and feature switch allow; and Equity as "Who owns the company"
- **AND** it has at most 10 items

#### Scenario: Expert navigation
- **GIVEN** a user in Expert mode
- **WHEN** the navigation is shown
- **THEN** it adds Accounting, Planning, Compliance and all sub-pages, with at most 13 items (§7.1)

#### Scenario: Permissions decide in either mode
- **WHEN** a user in either mode attempts an action
- **THEN** whether they may do it is decided by their permissions, not by the mode

### Requirement: Basic and Expert data entry
Basic mode SHALL use guided forms in plain words, one document at a time, and Expert mode SHALL add line-level and bulk entry.

Source: PRD-001 §7.8

#### Scenario: Basic data entry
- **GIVEN** a user in Basic mode
- **WHEN** they enter a document
- **THEN** a guided form asks for the contact, date, amount, category, GST code (suggested) and attachment, in plain words ("Money in", "Category"), one document at a time

#### Scenario: Expert data entry
- **GIVEN** a user in Expert mode
- **WHEN** they enter a document
- **THEN** they can also enter lines with accounts, dimensions, FX rate overrides, service periods and spreads, discounts, tax overrides and numbering
- **AND** they can use spreadsheet grids with paste, fill-down and `=` arithmetic (FR-UX-07)

### Requirement: Basic and Expert lists
Basic mode SHALL show simple tables and Expert mode SHALL show full grids.

Source: PRD-001 §7.8

#### Scenario: Basic lists
- **GIVEN** a user in Basic mode
- **WHEN** a list is shown
- **THEN** it is a simple table with the key columns, saved filters and a "needs attention" view

#### Scenario: Expert lists
- **GIVEN** a user in Expert mode
- **WHEN** a list is shown
- **THEN** it is a full grid with a column chooser, bulk edit, multi-sort, export and saved views (FR-UX-12)

### Requirement: Basic and Expert banking
Basic mode SHALL let the user categorise bank lines one at a time from suggestions, and Expert mode SHALL add bulk work, rules and the reconciliation workspace.

Source: PRD-001 §7.8

#### Scenario: Basic banking
- **GIVEN** a user in Basic mode
- **WHEN** they work on bank lines
- **THEN** they categorise one line at a time from suggestions, accept or change the suggestion, and match a line to an invoice

#### Scenario: Expert banking
- **GIVEN** a user in Expert mode
- **WHEN** they work on bank lines
- **THEN** they also have bulk categorise, split, transfers, the rules editor with regex and back-test (FR-BNK-05) and the reconciliation workspace

### Requirement: Basic and Expert reports
Basic mode SHALL offer a curated set of reports with plain-English explanations, and Expert mode SHALL add the full reporting set.

Source: PRD-001 §7.8

#### Scenario: Basic reports
- **GIVEN** a user in Basic mode
- **WHEN** they open Reports
- **THEN** they see the P&L, balance sheet, cash and runway, who owes us / whom we owe and the GST summary, each with a plain-English explanation

#### Scenario: Expert reports
- **GIVEN** a user in Expert mode
- **WHEN** they open Reports
- **THEN** they also have comparatives, dimension pivots, trial balance, general ledger, journals, the report builder (P2) and all exports

### Requirement: Basic and Expert ledger detail
In Basic mode the Ledger impact tab SHALL be collapsed with a one-line plain summary, and in Expert mode it SHALL be open with debits, credits and account codes.

Source: PRD-001 §7.8

#### Scenario: Basic ledger detail
- **GIVEN** a user in Basic mode
- **WHEN** they open a bill
- **THEN** the Ledger impact tab is collapsed, with a one-line plain summary such as "This bill adds S$1,090 to what we owe ABC"

#### Scenario: Expert ledger detail
- **GIVEN** a user in Expert mode
- **WHEN** they open a record
- **THEN** the Ledger impact tab is open with debits, credits and account codes (FR-UX-08 Accountant view)

### Requirement: Basic and Expert keyboard
Basic mode SHALL offer ⌘K and `?` only, plus ⌘J for the assistant panel and the mode-toggle shortcut (FR-UX-23), and Expert mode SHALL offer the full shortcut set (FR-UX-03/06), still remappable and switchable off.

Source: PRD-001 §7.8

#### Scenario: Basic keyboard
- **GIVEN** a user in Basic mode
- **WHEN** they press a keyboard shortcut
- **THEN** only ⌘K, `?`, ⌘J (the assistant panel) and the mode-toggle shortcut (FR-UX-23) work

#### Scenario: Expert keyboard
- **GIVEN** a user in Expert mode
- **WHEN** they use keyboard shortcuts
- **THEN** the full shortcut set of FR-UX-03 and FR-UX-06 works
- **AND** it can still be remapped and switched off

### Requirement: Basic and Expert density
Basic mode SHALL use the Comfortable density, and Expert mode SHALL default to Compact, which the user can change.

Source: PRD-001 §7.8

#### Scenario: Density by mode
- **WHEN** a user in Basic mode views a data page
- **THEN** it uses the Comfortable density
- **AND** in Expert mode it uses Compact by default, which the user can change

### Requirement: Basic and Expert AI assistant
In Basic mode the AI assistant SHALL give plain-language answers and suggestions, and in Expert mode it SHALL also show the ledger lines, account codes and query details behind each figure.

Source: PRD-001 §7.8

#### Scenario: AI answer by mode
- **WHEN** the AI assistant answers a question with a figure
- **THEN** in Basic mode the answer is in plain language
- **AND** in Expert mode it also shows the ledger lines, account codes and query details behind the figure

### Requirement: Basic and Expert settings
In Basic mode Settings SHALL show the profile, theme, notifications and connected apps, and in Expert mode everything the role allows.

Source: PRD-001 §7.8

#### Scenario: Settings by mode
- **WHEN** a user in Basic mode opens Settings
- **THEN** they see their profile, theme, notifications and connected apps
- **AND** in Expert mode they see everything their role allows

### Requirement: Basic and Expert help
In Basic mode help SHALL be in plain words with the first-run tour and guide pages for Basic features, and in Expert mode tooltips SHALL add the accounting detail with every guide page (§7.9) available.

Source: PRD-001 §7.8

#### Scenario: Basic help
- **GIVEN** a user in Basic mode
- **WHEN** they use help
- **THEN** tooltips are in plain words and they have the first-run tour and the guide pages for Basic features

#### Scenario: Expert help
- **GIVEN** a user in Expert mode
- **WHEN** they use help
- **THEN** tooltips add the accounting detail
- **AND** every guide page (§7.9) is available

### Requirement: Basic and Expert equity
In Basic mode Equity SHALL show ownership simply, and in Expert mode it SHALL add the full equity detail.

Source: PRD-001 §7.8

#### Scenario: Basic equity
- **GIVEN** a user in Basic mode
- **WHEN** they open Equity
- **THEN** they see the ownership chart and table, their holding, the latest round and a simple round simulator

#### Scenario: Expert equity
- **GIVEN** a user in Expert mode
- **WHEN** they open Equity
- **THEN** they also see classes and rights, instruments, the option pool, transactions, fully diluted views and the reconciliation (FR-EQ-27)

### Requirement: Plain view and Accountant view within the modes
The Plain view / Accountant view toggle (FR-UX-08) SHALL stay an independent toggle inside Expert mode, and Basic mode SHALL always use the Plain view.

Source: PRD-001 §7.8

#### Scenario: Toggle independent of the mode
- **GIVEN** a user in Expert mode
- **WHEN** they switch between Plain view and Accountant view
- **THEN** they stay in Expert mode

#### Scenario: Basic always uses Plain view
- **GIVEN** a user who chose Accountant view in Expert mode
- **WHEN** they switch to Basic mode
- **THEN** Plain view is used

### Requirement: FR-UX-23 Mode toggle
Each user SHALL switch between **Basic** and **Expert** mode instantly, and the choice SHALL be stored server-side as a user preference, the same on every device.

Source: PRD-001 §7.8 · Priority M · Phase P1

#### Scenario: Ways to switch
- **WHEN** a user wants to switch mode
- **THEN** they can use the segmented control in the header, the command palette ("Switch to Expert mode") or a shortcut

#### Scenario: Instant switch keeps the work
- **GIVEN** a user with filters set and an unsaved draft
- **WHEN** they switch mode
- **THEN** the page does not reload
- **AND** the current page, the filters and the unsaved draft are kept

#### Scenario: Same mode on every device
- **WHEN** a user switches mode on one device and signs in on another
- **THEN** the other device shows the same mode

#### Scenario: Mode always visible
- **WHEN** any page is shown
- **THEN** the current mode is visible in the header

### Requirement: FR-UX-24 Presentation only
The mode SHALL be presentation only: it SHALL never grant or remove a permission, never change validation, posting, approvals or the data saved, and **safety information SHALL never be hidden** in Basic mode.

Source: PRD-001 §7.8 · Priority M · Phase P1

#### Scenario: Same permissions and rules in both modes
- **WHEN** a user performs the same action in Basic and in Expert mode
- **THEN** the permissions, validation, posting, approvals and data saved are the same
- **AND** the action calls the same API in both modes

#### Scenario: Safety information in Basic mode
- **GIVEN** a user in Basic mode
- **WHEN** a page has ✖ or ⚠ statuses, approvals waiting, locked periods, compliance deadlines, AI labels or a books-health status
- **THEN** they are shown

### Requirement: FR-UX-25 Nothing silently hidden on records
When a record uses fields that Basic mode does not show, Basic mode SHALL show a disclosure that reveals them read-only, with "Open in Expert mode".

Source: PRD-001 §7.8 · Priority M · Phase P1

#### Scenario: Advanced details disclosure
- **GIVEN** a record with dimensions and an FX override entered by the accountant
- **WHEN** a user in Basic mode opens it
- **THEN** a "Has advanced details (2)" disclosure is shown
- **AND** opening it reveals those fields read-only, with "Open in Expert mode"

### Requirement: FR-UX-26 Discoverable growth
In Basic mode, search and the command palette SHALL still find Expert features and offer "Available in Expert mode: switch?" as one action, and contextual hints SHALL be dismissible, limited and switchable off.

Source: PRD-001 §7.8 · Priority M · Phase P1

#### Scenario: Expert feature found from Basic mode
- **GIVEN** a user in Basic mode
- **WHEN** search or the command palette finds an Expert feature
- **THEN** it offers "Available in Expert mode: switch?" as one action

#### Scenario: Contextual hints
- **WHEN** a contextual hint such as "You can split this line in Expert mode" is shown
- **THEN** it can be dismissed
- **AND** it appears at most once per session

#### Scenario: Hints turned off
- **WHEN** a user turns contextual hints off
- **THEN** no further hints are shown to them

### Requirement: FR-UX-27 Defaults by role
New users SHALL start in Basic or Expert mode according to their roles, with **Owner winning** when roles disagree, and the Administrator SHALL be able to change the default per role.

Source: PRD-001 §7.8 · Priority S · Phase P1

#### Scenario: Basic by default
- **WHEN** a new user's roles are among Owner, Bookkeeper, AR/AP specialist, Approver, Employee or Board viewer
- **THEN** they start in **Basic**

#### Scenario: Founders are Owners
- **WHEN** a founder joins
- **THEN** their default comes from their roles (usually Owner): "Founder" is not a role (§6.2)

#### Scenario: Expert by default
- **GIVEN** a new user who is not an Owner
- **WHEN** any of their roles is Accountant, Controller, Tax agent, Auditor or Payroll administrator
- **THEN** they start in **Expert**

#### Scenario: Owner wins
- **WHEN** a new user is Owner and also Payroll administrator
- **THEN** they start in Basic

#### Scenario: Administrator changes a role's default
- **WHEN** the Administrator changes the default mode for a role
- **THEN** new users with that role start in the new default

### Requirement: FR-UX-28 Pin a role to Basic
The Administrator SHALL be able to **pin** a role to Basic mode (for example Employee or Board viewer).

Source: PRD-001 §7.8 · Priority C · Phase P2

#### Scenario: Pinned users
- **GIVEN** the Administrator pinned the Employee role to Basic
- **WHEN** an employee uses Fyniq
- **THEN** they are in Basic mode
- **AND** they do not see the mode toggle

### Requirement: FR-UX-29 One feature registry
Every route, navigation item, field group, column set and action in `apps/web` SHALL be tagged `basic` or `expert` in a single registry.

Source: PRD-001 §7.8 · Priority M · Phase P1

#### Scenario: Untagged feature fails the build
- **WHEN** a route, navigation item, field group, column set or action has no `basic` or `expert` tag
- **THEN** a test fails

#### Scenario: Identical API requests in both modes
- **WHEN** the end-to-end test performs the same action in Basic and in Expert mode
- **THEN** it asserts that both send identical API requests (QA-UI-04)

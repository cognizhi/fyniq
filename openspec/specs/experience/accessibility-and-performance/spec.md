# experience/accessibility-and-performance Specification

## Purpose
The accessibility, localisation and front-end performance budgets every Fyniq screen must meet: WCAG 2.2 AA, full keyboard operation, focus never obscured, alternatives to dragging, accessible authentication, English (Singapore) with per-user formats, and load, interaction, grid and bundle-size budgets. Derived from PRD-001 §7.6.

## Requirements

### Requirement: NFR-A11Y-01 WCAG 2.2 level AA
The application SHALL meet WCAG 2.2 level AA throughout, tested automatically (axe) and manually each release.

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Automated accessibility test
- **WHEN** the automated accessibility tests (axe) run on the application
- **THEN** they report no WCAG 2.2 level AA violation

#### Scenario: Manual test each release
- **WHEN** a release is prepared
- **THEN** the application is tested manually against WCAG 2.2 level AA

### Requirement: NFR-A11Y-02 Keyboard, focus and screen-reader labels
The application SHALL be fully operable by keyboard with visible focus, and every control and chart SHALL have a screen-reader label.

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Keyboard only
- **WHEN** a user operates any page with the keyboard only
- **THEN** every function is available
- **AND** the focus is always visible

#### Scenario: Screen-reader labels
- **WHEN** a screen reader reaches a control or a chart
- **THEN** it announces the element's label

#### Scenario: Accessible alternative to a chart
- **WHEN** a screen-reader user needs a chart's data
- **THEN** the chart's table view is the accessible alternative

### Requirement: NFR-A11Y-03 Focus not obscured
The focused element SHALL never be hidden by sticky report headers and first columns, side-peek panels, the AI panel or toasts (WCAG 2.2 SC 2.4.11).

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Overlays never hide focus
- **WHEN** a side-peek panel, the AI panel or a toast is open and the user moves focus
- **THEN** the focused element stays visible

#### Scenario: Scrolling keeps the focused cell clear
- **GIVEN** a report with a sticky header and a sticky first column
- **WHEN** focus moves to a cell that would sit under a sticky region
- **THEN** the report scrolls so that the focused cell is clear of the sticky regions

### Requirement: NFR-A11Y-04 Dragging alternatives
Every dragging interaction SHALL have single-pointer and keyboard alternatives (WCAG 2.2 SC 2.5.7).

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Alternatives to dragging
- **WHEN** a user fills down, resizes a column, lays out dashboard tiles or resizes a split view
- **THEN** they can do it with a single pointer action without dragging, or with the keyboard (for example Ctrl+D to fill down)

### Requirement: NFR-A11Y-05 Accessible authentication
Sign-in and step-up SHALL allow password managers, paste and passkeys, with no cognitive tests (WCAG 2.2 SC 3.3.8).

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Password managers, paste and passkeys
- **WHEN** a user signs in or passes step-up
- **THEN** they can use a password manager, paste into the fields or use a passkey
- **AND** no cognitive test is asked

#### Scenario: Error summary after a failed submit
- **WHEN** a sign-in or step-up form is submitted with errors
- **THEN** an error summary takes focus
- **AND** it links to each field in error

### Requirement: NFR-L10N-01 Localisation
The application SHALL launch in English (Singapore) with all strings externalised for translation, per-user number and date formats, and a time zone per organisation.

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Strings externalised
- **WHEN** a user-facing string is added
- **THEN** it is externalised for translation

#### Scenario: Default date format
- **GIVEN** a user who has not chosen a date format
- **WHEN** the date 12 October 2026 is shown
- **THEN** it reads "12 Oct 2026"

#### Scenario: Per-user formats
- **WHEN** a user chooses a number or date format
- **THEN** numbers and dates are shown to that user in it

#### Scenario: Organisation time zone
- **GIVEN** a new organisation with no time zone chosen
- **WHEN** times are shown
- **THEN** they use Asia/Singapore
- **AND** each organisation can set its own time zone

### Requirement: NFR-PERF-UI-01 Page load and interaction budgets
The application SHALL keep p75 Largest Contentful Paint ≤ 1.5 s, interaction to next paint ≤ 200 ms and route changes ≤ 300 ms with cached data.

Source: PRD-001 §7.6 · Priority M · Phase P1

#### Scenario: Largest Contentful Paint
- **WHEN** pages are loaded on a mid-range laptop over 4G
- **THEN** the 75th percentile Largest Contentful Paint is ≤ 1.5 s

#### Scenario: Interaction to next paint
- **WHEN** a user interacts with a page
- **THEN** the interaction to next paint is ≤ 200 ms

#### Scenario: Route change with cached data
- **GIVEN** the data for the next route is cached
- **WHEN** the user changes route
- **THEN** the route change completes in ≤ 300 ms

### Requirement: NFR-PERF-UI-02 Grid scrolling
Grids SHALL stay responsive, scrolling at ≥ 50 fps with 100,000 rows.

Source: PRD-001 §7.6 · Priority S · Phase P1

#### Scenario: Scrolling a large grid
- **GIVEN** a grid holding 100,000 rows
- **WHEN** the user scrolls it
- **THEN** scrolling runs at 50 frames per second or more

### Requirement: NFR-PERF-UI-03 Code splitting and bundle budget
The web app SHALL split code by route and keep the initial JavaScript for the shell and Home ≤ 250 KB compressed.

Source: PRD-001 §7.6 · Priority S · Phase P1

#### Scenario: Initial bundle size
- **WHEN** the shell and Home load
- **THEN** the initial JavaScript is ≤ 250 KB compressed

#### Scenario: Heavy modules on demand
- **WHEN** a page needs AG Grid, ECharts or the PDF preview
- **THEN** the module is loaded on demand, not in the initial bundle

#### Scenario: Preload on intent
- **WHEN** the user hovers over or focuses a link to a page that needs a heavy module
- **THEN** the module is preloaded

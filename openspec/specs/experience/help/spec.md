# experience/help Specification

## Purpose
How Fyniq explains itself where the user is: tooltips and info popovers on every page, one help registry, the in-app user guide, context-aware help, the first-run walkthrough, feature tours and "What's new", and practising safely in the sample company. Derived from PRD-001 §7.9.

## Requirements

### Requirement: FR-UX-30 Tooltips on every page
Every field label, column header, status chip, stat tile, chart, icon-only control and accounting term SHALL have help: a tooltip for a short hint, or an ⓘ info popover; help SHALL supplement the visible label and never replace it.

Source: PRD-001 §7.9 · Priority M · Phase P1

#### Scenario: Info popover content
- **WHEN** the user opens an ⓘ info popover
- **THEN** it says what the item is, why it matters, gives an example and links "Learn more" to the guide page

#### Scenario: Help never replaces the label
- **WHEN** an element has help
- **THEN** its visible label is still shown

#### Scenario: Wording by mode
- **WHEN** help is shown in Basic mode
- **THEN** it is worded plainly
- **AND** in Expert mode it adds the accounting detail (account, posting, rule ID)

#### Scenario: Hover, focus and dismissal
- **WHEN** the user hovers over or moves keyboard focus to an element with help
- **THEN** the help is shown
- **AND** it can be dismissed with Esc, can itself be hovered and stays until dismissed (WCAG 2.2 SC 1.4.13)

#### Scenario: Help on touch screens
- **WHEN** the user taps an ⓘ target on a touch screen
- **THEN** the help opens
- **AND** the ⓘ target is at least 24 × 24 px (SC 2.5.8)

### Requirement: FR-UX-31 One help registry
Every help text SHALL have a key in the feature registry (FR-UX-29), so that the Basic/Expert tag, the tooltip, the guide page and the tour step of a feature are defined in one place.

Source: PRD-001 §7.9 · Priority M · Phase P1

#### Scenario: Help text style
- **WHEN** a help text is written
- **THEN** it is in plain English and uses the UI's own names

#### Scenario: Accountant review of rules
- **WHEN** a help text states an accounting or statutory rule
- **THEN** the accountant reviews it

#### Scenario: Ready for translation
- **WHEN** help text is added
- **THEN** it is referenced by key, with no help text in code

#### Scenario: Missing help key fails the build
- **WHEN** a field, column, chip, tile or route has no help key
- **THEN** a test fails (QA-UI-05)

### Requirement: FR-UX-32 User guide
The system SHALL include a user guide for the entire platform in the app at `/help`, written in Markdown in `docs/user-guide/`, built into the web app (no external CMS) and versioned with each release.

Source: PRD-001 §7.9 · Priority M · Phase P1

#### Scenario: Ways into the guide
- **WHEN** the user presses the header **Help** button, chooses "Help: …" in ⌘K or follows a "Learn more" link
- **THEN** the user guide opens at `/help`

#### Scenario: Guide contents
- **WHEN** the user guide is opened
- **THEN** it has getting started; one page per module and per common task ("How do I record a bill?"); concepts in plain words (money in and out, debits and credits, statuses, Basic and Expert modes, approvals, AI proposals, founders' money); Singapore guides (GST, CPF, filings); keyboard shortcuts; the glossary (§21); and **What's new**, built from the release notes

#### Scenario: Search, links and print
- **WHEN** the user uses the guide
- **THEN** it is searchable, deep-linkable and printable
- **AND** it is filtered by the user's role and mode

#### Scenario: Screenshots never go stale
- **WHEN** a release is built
- **THEN** the guide's screenshots are regenerated from the end-to-end tests in light and dark themes

### Requirement: FR-UX-33 Context-aware help
The Help panel SHALL open on the guide page for the current screen, and the AI assistant SHALL answer "how do I…" questions from the user guide with links to the pages it used.

Source: PRD-001 §7.9 · Priority S · Phase P1

#### Scenario: Help panel opens on the current screen's page
- **WHEN** the user opens the Help panel
- **THEN** it shows the guide page mapped to the current route in the registry

#### Scenario: AI answers how-to questions from the guide
- **WHEN** the user asks the AI assistant "how do I…"
- **THEN** it answers from the user guide through the compiled knowledge base (§9.11; the curated knowledge base of AI-25)
- **AND** it links to the pages it used

### Requirement: FR-UX-34 First-run walkthrough
On a user's first sign-in, the system SHALL offer an optional animated tour of the real interface of at most seven steps, about 2 minutes, which never changes business data and never clicks on the user's behalf.

Source: PRD-001 §7.9 · Priority M · Phase P1

#### Scenario: Offered at first sign-in
- **WHEN** a user signs in for the first time
- **THEN** the tour is offered
- **AND** the user may decline it

#### Scenario: Tour steps
- **WHEN** the tour runs
- **THEN** a spotlight moves to each element with a short caption: the Basic/Expert toggle, Home, Inbox and approving, recording money in and out, ⌘K search, the AI panel and Help, seven steps as in the prototype

#### Scenario: Role variants
- **WHEN** the tour runs for a founder or owner, accountant, employee, approver or board viewer
- **THEN** that role's variant is used, which may swap steps but never exceeds seven

#### Scenario: Skip, resume and replay
- **WHEN** the user skips the tour at any step, comes back later or chooses "Take the tour again" in Help
- **THEN** the tour stops, resumes or replays
- **AND** the tour progress is stored per user on the server

#### Scenario: No business data changes
- **WHEN** the tour runs
- **THEN** only the user's own tour progress is saved
- **AND** the tour never clicks on the user's behalf

#### Scenario: Reduced motion
- **GIVEN** `prefers-reduced-motion` is set
- **WHEN** the tour runs
- **THEN** it shows the same steps without animation

#### Scenario: Keyboard and screen readers
- **WHEN** the tour runs
- **THEN** it is fully operable with Next, Back and Esc from the keyboard and announces each step to screen readers
- **AND** it moves focus to each step and back to where it started, never hiding the focused element (SC 2.4.11)

### Requirement: FR-UX-35 Feature tours and "What's new"
The system SHALL offer a short tip-led tour the first time a user opens a feature or after a release changes it, and SHALL show release highlights once as a "What's new" card.

Source: PRD-001 §7.9 · Priority S · Phase P2

#### Scenario: Feature tip
- **WHEN** a user opens a feature for the first time, or after a release changed it
- **THEN** a one- to three-step tip offers a short tour

#### Scenario: What's new card
- **WHEN** a release has highlights
- **THEN** they appear once as a dismissible "What's new" card linked to the guide

#### Scenario: Tips turned off
- **WHEN** a user turns tips off
- **THEN** no feature tips are shown to them

### Requirement: FR-UX-36 Practice safely
Tours and guide pages SHALL be able to open the sample company (FR-UX-10) in a separate, clearly labelled organisation, so users can try a task without touching the real books.

Source: PRD-001 §7.9 · Priority S · Phase P1

#### Scenario: Practise in the sample company
- **WHEN** the user chooses to practise a task from a tour or a guide page
- **THEN** the sample company opens as a separate, clearly labelled organisation
- **AND** nothing they do there changes the real books

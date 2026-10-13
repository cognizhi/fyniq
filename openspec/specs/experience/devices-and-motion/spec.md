# experience/devices-and-motion Specification

## Purpose
How every Fyniq screen works on phones, foldables, tablets and desktops with the same information and safety cues, through three device classes with their own navigation and layout, and how motion stays minimal, purposeful and switchable. Derived from PRD-001 §7.10.

## Requirements

### Requirement: Compact device class
Below 600 CSS px wide, the Compact device class SHALL use a top bar, a bottom bar of at most five destinations and a one-column layout.

Source: PRD-001 §7.10

#### Scenario: Compact navigation
- **GIVEN** a screen less than 600 CSS px wide
- **WHEN** a page is shown
- **THEN** a top bar holds the menu, search and the AI
- **AND** a bottom bar holds at most five destinations: Home, Inbox, the role's main area, Banking and More

#### Scenario: Compact layout
- **GIVEN** a screen less than 600 CSS px wide
- **WHEN** a page is shown
- **THEN** it is one column, and lists and grids become cards
- **AND** statements scroll inside their own region with a sticky first column

#### Scenario: Panels open full screen
- **GIVEN** a screen less than 600 CSS px wide
- **WHEN** a side panel or the AI panel opens
- **THEN** it opens full screen

#### Scenario: Compact reference frames
- **WHEN** a screen is designed and checked for the Compact class
- **THEN** it uses the reference frames phone 390 × 844 and foldable folded (cover screen) 344 × 882 CSS px

### Requirement: Medium device class
From 600 to 1023 CSS px wide, the Medium device class SHALL use a navigation rail and show list and detail side by side where a screen has both.

Source: PRD-001 §7.10

#### Scenario: Navigation rail
- **GIVEN** a screen 600 to 1023 CSS px wide
- **WHEN** a page is shown
- **THEN** navigation is a rail of icons with labels

#### Scenario: List and detail side by side
- **GIVEN** a screen 600 to 1023 CSS px wide
- **WHEN** a screen with a list and a detail is shown
- **THEN** the list and the detail are side by side

#### Scenario: Panels open as sheets
- **GIVEN** a screen 600 to 1023 CSS px wide
- **WHEN** side-peek or the AI panel opens
- **THEN** it opens as a sheet over the content

#### Scenario: Medium reference frames
- **WHEN** a screen is designed and checked for the Medium class
- **THEN** it uses the reference frames foldable unfolded 690 × 840 and tablet portrait 834 × 1194 CSS px

### Requirement: Expanded device class
From 1024 CSS px wide, the Expanded device class SHALL use the sidebar and the layouts of §7.1–§7.4, with the AI panel and side-peek docked beside the content.

Source: PRD-001 §7.10

#### Scenario: Sidebar width
- **GIVEN** a screen at least 1024 CSS px wide
- **WHEN** a page is shown
- **THEN** the sidebar is 240 px wide
- **AND** below 1280 px it is collapsed to 64 px

#### Scenario: Docked panels
- **GIVEN** a screen at least 1024 CSS px wide
- **WHEN** the AI panel or side-peek opens
- **THEN** it docks beside the content

#### Scenario: Expanded reference frame
- **WHEN** a screen is designed and checked for the Expanded class
- **THEN** it uses the reference frame desktop 1440 × 900 CSS px, the class into which tablet landscape and laptops fall

### Requirement: FR-UX-37 Responsive at every size
Every screen SHALL work from 320 px wide (small phones and foldable cover screens) to large desktops, in portrait and landscape and in the three device classes, with no horizontal page scroll, keeping the same information and safety cues at every size.

Source: PRD-001 §7.10 · Priority M · Phase P1

#### Scenario: No horizontal page scroll
- **WHEN** a wide statement or grid is shown on a narrow screen
- **THEN** it scrolls inside its own region
- **AND** the page itself does not scroll horizontally

#### Scenario: Same information in every class
- **WHEN** the same screen is shown in the Compact, Medium and Expanded classes
- **THEN** the same data, statuses, approvals and safety information appear in each, as FR-UX-24 requires for modes; only the layout changes

#### Scenario: Touch targets
- **WHEN** a screen is used on a touch screen
- **THEN** touch targets are at least 44 × 44 px
- **AND** the 24 × 24 px pointer minimum still applies

#### Scenario: Inline info buttons on touch screens
- **GIVEN** an inline ⓘ info button (FR-UX-30) on a touch screen
- **WHEN** it is drawn
- **THEN** it may stay 24 × 24 px visually as long as its hit area reaches 44 × 44 px

#### Scenario: Folding, unfolding or rotating
- **GIVEN** a page with a scroll position, filters and an unsaved draft
- **WHEN** the device is folded, unfolded or rotated
- **THEN** the page, the scroll position, the filters and the unsaved draft are kept

#### Scenario: Spanning a hinge
- **GIVEN** a device that spans a hinge
- **WHEN** a two-pane layout is shown
- **THEN** the panes split at the hinge (CSS viewport segments)
- **AND** nothing is placed across the hinge

### Requirement: FR-UX-38 Minimal motion, switchable
The system SHALL use only a small, fixed catalogue of transitions that help people follow what changed, and the **Administrator** SHALL be able to set organisation motion to **On** (the recommended default) or **Off**.

Source: PRD-001 §7.10 · Priority S · Phase P1

#### Scenario: Motion catalogue
- **WHEN** something on screen changes
- **THEN** motion is used only for page and panel entrances (a fade with an 8 px rise or a slide, 120–240 ms, on `transform` and `opacity` only), list rows entering in a short stagger (at most 30 ms per row and 8 rows), skeletons while loading, status-chip and count changes, the Home pulse ticker and the "Books closed" moment

#### Scenario: Motion never gets in the way
- **WHEN** motion plays
- **THEN** it never delays input and never carries meaning alone
- **AND** it never loops, except progress indicators

#### Scenario: Organisation switch
- **WHEN** the Administrator sets motion to On or Off in Settings › Appearance and motion
- **THEN** it applies to the whole organisation
- **AND** the change is audited and follows FR-ORG-08

#### Scenario: User and system preferences
- **GIVEN** organisation motion is On
- **WHEN** a user chooses Reduce motion, or `prefers-reduced-motion` is set
- **THEN** motion is reduced for that user; `prefers-reduced-motion` always wins

#### Scenario: Off and Reduce show final states
- **WHEN** motion is Off or reduced
- **THEN** final states render immediately

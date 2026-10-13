# experience/visual-design Specification

## Purpose
How Fyniq looks: the "calm confidence with a spark" direction, readable and serious about numbers, with its personality from colour, motion and craft rather than decoration; themes, surfaces, the teal brand accent and the violet AI accent, typography, icons, numbers, motion, density, illustration, signature moments, design tokens, the design system files and the screen designs. Derived from PRD-001 §7.4.

## Requirements

### Requirement: Themes
The system SHALL offer **Light**, **Dark** and **System** themes, switchable from the header and the command palette, plus a **high-contrast** theme, and SHALL support Windows forced-colours mode.

Source: PRD-001 §7.4

#### Scenario: Switching theme
- **WHEN** the user picks Light, Dark or System from the header or the command palette
- **THEN** that theme applies
- **AND** System follows the operating system's setting

#### Scenario: High-contrast theme
- **WHEN** `prefers-contrast: more` is set, or the user turns on high contrast
- **THEN** the high-contrast theme applies
- **AND** text meets a contrast ratio of 7:1

#### Scenario: Forced-colours mode
- **WHEN** Windows forced-colours mode is on
- **THEN** the app uses system colours
- **AND** focus stays visible

#### Scenario: Dark mode has its own colour steps
- **WHEN** the Dark theme is shown
- **THEN** it uses its own selected colour steps, validated against the dark surface, not an automatic inversion

### Requirement: Surfaces
Light theme surfaces SHALL be warm paper white with soft elevation, and Dark theme surfaces SHALL be deep blue-black ("ink") with layered surfaces, not pure black.

Source: PRD-001 §7.4

#### Scenario: Surfaces per theme
- **WHEN** a page is shown in the Light theme
- **THEN** its surfaces are warm paper white with soft elevation
- **AND** in the Dark theme they are layered deep blue-black ("ink") surfaces, never pure black

### Requirement: Brand accent
**Deep teal** SHALL mark primary actions, links, selection and focus rings, with one primary action per view.

Source: PRD-001 §7.4

#### Scenario: Teal for actions
- **WHEN** a view shows a primary action, a link, a selection or a focus ring
- **THEN** it uses the deep teal brand accent

#### Scenario: One primary action per view
- **WHEN** a view is shown
- **THEN** it has at most one primary action

### Requirement: AI accent
**Violet** SHALL be used **only** for AI-originated content, always with an "AI" label, so AI content is never mistaken for human or posted data.

Source: PRD-001 §7.4

#### Scenario: Violet marks AI content
- **WHEN** an assistant message, proposal, AI suggestion or AI badge is shown
- **THEN** it uses the violet accent with an "AI" label

#### Scenario: Violet is not used for anything else
- **WHEN** content that did not originate from the AI is shown
- **THEN** it does not use the violet accent

#### Scenario: Gradient only in two places
- **WHEN** the violet-to-teal gradient is used
- **THEN** it appears only on the Home "pulse" card and the AI panel header
- **AND** never on controls or text backgrounds

### Requirement: Typography
The UI SHALL use **Inter** with tabular figures (`tnum`) in every table, total and axis, and **JetBrains Mono** for codes, references and IDs, both SIL OFL and **self-hosted**, with no third-party font requests.

Source: PRD-001 §7.4

#### Scenario: Fonts are self-hosted
- **WHEN** a page loads its fonts
- **THEN** Inter and JetBrains Mono come from Fyniq's own host with `font-display: swap`
- **AND** no third-party font request is made

#### Scenario: Tabular figures and monospace codes
- **WHEN** a table, total or axis shows figures, or a code, reference or ID is shown
- **THEN** figures use Inter's tabular figures and codes, references and IDs use JetBrains Mono

#### Scenario: Recorded alternative
- **WHEN** Inter proves too narrow in dense grids
- **THEN** uipro's "Financial Trust" pairing (IBM Plex Sans and Mono) is the recorded alternative

### Requirement: Icons
The system SHALL use one SVG icon set, **Lucide**, in 16, 20 and 24 px sizes with a 1.5 px stroke, and SHALL use no emoji as structural icons.

Source: PRD-001 §7.4

#### Scenario: One icon set
- **WHEN** an icon is shown in the UI
- **THEN** it comes from Lucide at a 16, 20 or 24 px token size with a 1.5 px stroke
- **AND** no emoji is used as a structural icon

#### Scenario: Icons and assistive technology
- **WHEN** a decorative icon is shown beside text
- **THEN** it is `aria-hidden`
- **AND** an icon-only button carries an accessible name

### Requirement: Numbers
Numbers SHALL be right-aligned, tabular and with thousands separators, with negative numbers shown as "−1,234.56" or "(1,234.56)" per user preference.

Source: PRD-001 §7.4

#### Scenario: Negative number format
- **GIVEN** a user who prefers parentheses for negative numbers
- **WHEN** −1,234.56 is shown
- **THEN** it reads "(1,234.56)"
- **AND** for a user who prefers the minus sign it reads "−1,234.56"

#### Scenario: Colouring is never the only cue
- **WHEN** positive and negative colouring is turned on
- **THEN** the sign or parentheses still show whether the number is negative

#### Scenario: Clipboard and exports
- **WHEN** numbers are copied or exported
- **THEN** they follow FR-UX-20

### Requirement: Motion
Motion SHALL be short (120–240 ms by distance: 120 fast, 180 base, 240 slow; exits faster than entrances) and purposeful, on `transform` and `opacity` only, with the motion catalogue and the organisation-wide switch in FR-UX-38 (§7.10).

Source: PRD-001 §7.4

#### Scenario: Number tickers
- **WHEN** a figure changes on Home
- **THEN** a number ticker may animate it
- **AND** figures in reports never use number tickers

#### Scenario: Reduce motion
- **WHEN** "reduce motion" is on
- **THEN** final states render immediately

### Requirement: Density
Each user SHALL choose a **Comfortable** (default, 40 px rows) or **Compact** (32 px rows) density on a 4 px grid, and every pointer target SHALL stay at least 24 × 24 px (WCAG 2.2 SC 2.5.8).

Source: PRD-001 §7.4

#### Scenario: Density choice
- **WHEN** a user chooses Comfortable or Compact
- **THEN** table rows are 40 px or 32 px high

#### Scenario: Layout measures
- **WHEN** the shell is shown on an Expanded screen
- **THEN** the header is 56 px high and the sidebar 240 px wide (64 px collapsed), with spacing of 8–32 px in data views; layouts per device class follow §7.10

#### Scenario: Pointer targets
- **WHEN** any density is used
- **THEN** every pointer target is at least 24 × 24 px

### Requirement: Illustration and signature moments
Friendly line illustrations SHALL appear for empty states and onboarding and never inside reports, with a Home "pulse" card and a "books closed" moment as the signature moments.

Source: PRD-001 §7.4

#### Scenario: Illustrations
- **WHEN** an empty state or onboarding is shown
- **THEN** it may use a friendly line illustration
- **AND** reports contain no illustrations

#### Scenario: Signature moments
- **WHEN** Home is shown, or the month-end books are closed
- **THEN** Home shows the "pulse" card (cash, runway, health) with a subtle gradient, and closing the books shows a satisfying "books closed" moment
- **AND** elsewhere there is no confetti

### Requirement: Design tokens
Design tokens SHALL be defined once and used everywhere, covering colour, typography scale, spacing (4 px grid), radius, elevation and motion, with light and dark values for every colour token.

Source: PRD-001 §7.4

#### Scenario: Semantic colour tokens
- **WHEN** the colour tokens are defined
- **THEN** they include background, surface-1/2/3, text-primary/secondary/muted, border, accent, positive, negative, status good/warning/serious/critical and the AI accent, each with a light and a dark value

#### Scenario: No raw colours in components
- **WHEN** a component in `apps/web` sets a colour
- **THEN** it uses a token from `tokens.css`, which the web app imports, never a raw colour value

#### Scenario: Visual regression in both themes
- **WHEN** the visual-regression test runs
- **THEN** it renders the key pages in the light and the dark theme (QA-RPT-04)

### Requirement: Design system files
The design system SHALL live in `docs/design-system/fyniq/`, with `MASTER.md`, `pages/*.md`, `tokens.css` and the contrast validator `scripts/check_contrast.py`.

Source: PRD-001 §7.4

#### Scenario: Global rules and page overrides
- **WHEN** a page's design is decided
- **THEN** `MASTER.md` gives the global rules (style: uipro "Data-Dense Dashboard" on a minimal Swiss base; colour roles, type scale, density, motion, components, accessibility, anti-patterns)
- **AND** a page file in `pages/*.md` (Home, Inbox and proposal review, bank categorisation, reports) wins over `MASTER.md`

#### Scenario: Token file
- **WHEN** `tokens.css` is read
- **THEN** it holds OKLCH semantic tokens for `:root` (light), `.dark` and `[data-contrast="more"]`, exposed to Tailwind 4 with `@theme inline`

#### Scenario: Contrast validator in CI
- **WHEN** CI runs `scripts/check_contrast.py` with FR-VIZ-07's palette check
- **THEN** every foreground/background token pair must meet its WCAG target in every theme
- **AND** a regression fails the build

### Requirement: Screen designs
Every screen SHALL be designed at the five reference frames of §7.10 in light and dark, in Claude Design canvases built from `docs/design/screens/`, which supersede the prototype as the visual reference.

Source: PRD-001 §7.4

#### Scenario: Screen canvases
- **WHEN** a screen is designed
- **THEN** it is drawn at the five reference frames in light and dark from the screen inventory, the shared stylesheet and the canvas sources in `docs/design/screens/`

#### Scenario: Canvases supersede the prototype
- **WHEN** the static prototype in `docs/design/prototype/` and a canvas differ
- **THEN** the canvas is the visual reference

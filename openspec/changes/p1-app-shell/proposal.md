# Proposal

## Why

P0's web app is a shell: sign-in, first-run setup, recovery codes and Home, with no router, grid or
design-system components. Every P1 screen sits inside the same frame: workspaces by role, Basic and
Expert modes, help on every page, themes, devices, motion and accessibility. The frame is built once,
before wave 2's screens, so they inherit it rather than each inventing it (§7).

## What Changes

- **Navigation (§7.1, §7.2):**
  - the sidebar of at most 13 items, the personal section and badges;
  - workspaces by role, cut to the person's rights (FR-UX-40), and the personal workspace for people
    who only claim (FR-UX-39);
  - the record page anatomy, and the AI side panel as a placeholder until wave 6.
- **Finding and moving:**
  - the command palette (FR-UX-01), global search (FR-UX-02) and Inbox triage, whose approve shortcut
    opens the confirmation (FR-UX-03);
  - drill-down to the Trace panel (FR-UX-04) and side peeks with split view (FR-UX-05);
  - keyboard-first entry with the shortcut sheet (FR-UX-06).
- **Grids and records:**
  - spreadsheet-like grids virtualised to 100,000 rows (FR-UX-07, NFR-PERF-UI-02);
  - Plain and Accountant views (FR-UX-08) and status chips with text and icon (FR-UX-09);
  - guided set-up with the sample company (FR-UX-10) and teaching empty states (FR-UX-11);
  - favourites, recents and saved views (FR-UX-12), and comments with mentions (FR-UX-13);
  - nothing hard-deleted (FR-UX-14).
- **Correctness in the browser:**
  - the in-app notification entry (FR-UX-15, P1 part);
  - live updates through a change feed, with conflicts caught by `If-Match` (FR-UX-19);
  - clipboard fidelity (FR-UX-20) and accessible live counts (FR-UX-21);
  - no optimistic money (FR-UX-22), and display that never re-rounds (RND-DISPLAY).
- **Modes (§7.8):**
  - the mode toggle stored server-side (FR-UX-23);
  - presentation only, never hiding safety information (FR-UX-24);
  - the disclosure for hidden fields (FR-UX-25) and discoverable growth (FR-UX-26);
  - defaults by role, with the Owner winning (FR-UX-27);
  - one feature registry (FR-UX-29), and the 13 unnumbered Basic/Expert requirements.
- **Help (§7.9):**
  - tooltips (FR-UX-30) and one help registry (FR-UX-31);
  - the user guide at `/help` (FR-UX-32) and context-aware help (FR-UX-33). The AI half of FR-UX-33
    is wave 6;
  - the first-run tour (FR-UX-34), practice in the sample company (FR-UX-36), and the "Needs
    additional verification" banner (FR-UX-41).
- **Look and devices (§7.4, §7.10):**
  - the 13 visual-design requirements: themes, including high contrast and forced colours; surfaces;
    teal and violet; Inter and JetBrains Mono, self-hosted; Lucide; numbers; motion; density; tokens;
    design-system files; screen designs;
  - the three device classes and responsive layouts from 320 px (FR-UX-37);
  - minimal, switchable motion (FR-UX-38).
- **Quality (§13):** WCAG 2.2 AA (NFR-A11Y-01…05), localisation-ready strings (NFR-L10N-01), and the
  page-load, interaction and bundle budgets (NFR-PERF-UI-01, 03).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `experience/navigation`: FR-UX-01…15 and 19…22, FR-UX-39 and 40, plus 8 unnumbered;
- `experience/modes`: FR-UX-23…27 and 29, plus 13 unnumbered;
- `experience/help`: FR-UX-30…34, 36 and 41;
- `experience/visual-design`: 13 unnumbered;
- `experience/devices-and-motion`: FR-UX-37 and 38, plus 3 unnumbered;
- `experience/accessibility-and-performance`: NFR-A11Y-01…05, NFR-L10N-01 and NFR-PERF-UI-01…03;
- `ledger/money-and-rounding`: RND-DISPLAY.

## Impact

- **Code:**
  - `apps/web` gains TanStack Router and Query, shadcn/ui on Base UI with Tailwind CSS 4, cmdk, the
    grid and the React Compiler, as ADR-T1 chose;
  - Vitest and axe for component tests, and Playwright for journeys;
  - the API gains the operations for preferences, saved views, comments, search, the change feed and
    the feature registry's server half.
- **Screens, 18 as designed:** app-shell, appearance-motion, command-palette, department-home,
  edit-conflict, first-run-tour, global-search, guided-setup, help-guide, inbox, invoices-list (the
  list frame; its data arrives with `p1-sales`), journal-editor (the grid frame), my-shares,
  not-part-of-access, profile-preferences, shortcut-sheet, trace-panel and void-confirmation.
- **Needs the founder:** an AG Grid Enterprise licence (about US$999 per front-end developer, §12.6)
  before range selection, fill and Excel-style clipboard. Until then the grid runs on the Community
  edition. The licence key is a CI secret and is never committed, because the repository is public.

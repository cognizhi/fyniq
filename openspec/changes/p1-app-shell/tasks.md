# Tasks

## 1. Foundations of the web app

- [ ] 1.1 Add the ADR-T1 libraries (TanStack Router and Query, shadcn/ui on Base UI, Tailwind CSS 4 from the design tokens, cmdk, the React Compiler), Vitest with axe, and Inter and JetBrains Mono self-hosted. All versions are pinned and at least 2 weeks old. Tests `// req: experience/visual-design Typography` and `… Design tokens`: no third-party font request, and every colour comes from a token
- [ ] 1.2 Write the feature registry and its test: a route or action without a mode tag, a help key or a guide page fails. Tests `// req: FR-UX-29` and `// req: FR-UX-31`
- [ ] 1.3 Themes (light, dark, system, high contrast, forced colours), surfaces, the teal and violet rules, icons, numbers, density and motion. Tests `// req: experience/visual-design <title>` for each of the 13, and `// req: FR-UX-38`

## 2. Navigation and workspaces

- [ ] 2.1 The sidebar, personal section, badges and record page anatomy, and workspaces cut to the person's rights from the session's effective rights. Tests `// req: FR-UX-40`, `// req: FR-UX-39` and `// req: experience/navigation <title>`; a hidden endpoint called directly returns 403
- [ ] 2.2 The command palette, global search in PostgreSQL under row-level security, and Inbox triage whose approve shortcut opens the confirmation. Tests `// req: FR-UX-01`, `// req: FR-UX-02` and `// req: FR-UX-03`
- [ ] 2.3 Drill-down to the Trace panel, side peeks and split view, keyboard-first entry and the shortcut sheet. Tests `// req: FR-UX-04`, `// req: FR-UX-05` and `// req: FR-UX-06`

## 3. Grids, records and correctness

- [ ] 3.1 The grid (Community edition until the licence arrives) virtualised to 100,000 rows, and statement tables with TanStack. Tests `// req: FR-UX-07` and `// req: NFR-PERF-UI-02` (≥ 50 fps in Playwright tracing)
- [ ] 3.2 Plain and Accountant views, status chips, guided set-up with the sample company, and teaching empty states. Tests `// req: FR-UX-08`, `// req: FR-UX-09`, `// req: FR-UX-10` and `// req: FR-UX-11`
- [ ] 3.3 Favourites, recents and saved views shareable by link, comments with mentions, and no hard delete. Tests `// req: FR-UX-12`, `// req: FR-UX-13` and `// req: FR-UX-14`
- [ ] 3.4 The live-update feed with `If-Match` conflicts, clipboard fidelity, the polite status region, no optimistic money (lint rule) and no re-rounding. Tests `// req: FR-UX-19`, `// req: FR-UX-20`, `// req: FR-UX-21`, `// req: FR-UX-22` and `// req: RND-DISPLAY`, plus the in-app notification entry `// req: FR-UX-15` (P1 part)

## 4. Modes and help

- [ ] 4.1 The mode toggle stored server-side, presentation only, the disclosure of hidden fields, discoverable growth and defaults by role. Tests `// req: FR-UX-23` through `// req: FR-UX-27`, and `// req: experience/modes <title>` for each of the 13 unnumbered requirements, with a check that safety information shows in Basic mode
- [ ] 4.2 Tooltips, `/help` built from `docs/user-guide/`, context-aware help, the first-run tour, practice in the sample company, and the FR-UX-41 banner from the review register. Tests `// req: FR-UX-30`, `// req: FR-UX-32`, `// req: FR-UX-33` (guide half), `// req: FR-UX-34`, `// req: FR-UX-36` and `// req: FR-UX-41`

## 5. Devices, accessibility and budgets

- [ ] 5.1 The three device classes and layouts from 320 px, portrait and landscape, with no horizontal scroll. Tests `// req: FR-UX-37` and `// req: experience/devices-and-motion <class> device class` at the five frames (QA-UI-07)
- [ ] 5.2 WCAG 2.2 AA with axe on every shell screen, keyboard and focus tests, focus not obscured, dragging alternatives and accessible authentication. Tests `// req: NFR-A11Y-01` through `// req: NFR-A11Y-05`
- [ ] 5.3 Externalised strings with per-user formats and the organisation's time zone, and the page-load, interaction and bundle budgets measured in CI. Tests `// req: NFR-L10N-01`, `// req: NFR-PERF-UI-01` and `// req: NFR-PERF-UI-03`

## 6. Screens

- [ ] 6.1 Build the 18 shell screens as designed. Each has help keys, a `docs/user-guide/` page compiled into `kb/`, and a tour step where it is on the first-run path
- [ ] 6.2 Finish with `make check`, `make test` and `make test-e2e`

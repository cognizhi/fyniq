# Tasks

## 1. Foundations of the web app

- [x] 1.1 Add the ADR-T1 libraries (TanStack Router and Query, shadcn/ui on Base UI, Tailwind CSS 4 from the design tokens, cmdk, the React Compiler), Vitest with axe, and Inter and JetBrains Mono self-hosted. All versions are pinned and at least 2 weeks old. Tests `// req: experience/visual-design Typography` and `… Design tokens`: no third-party font request, and every colour comes from a token
  (20 Oct 2026: TanStack Router 1.170.41 (code-based routes built from the registry; Sign-in and Install load on
  demand) and Query 5.104.1; Base UI 1.8.0 under `CSPProvider disableStyleElements` (the CSP allows no inline style
  element), with shadcn/ui-style components (`src/components/ui/button.tsx`, `cn` with tailwind-merge 3.7.0, clsx
  2.1.1, class-variance-authority 0.7.1); Tailwind CSS 4.3.3 through `@tailwindcss/vite`, mapped from `tokens.css`,
  without preflight (the screen kit's resets stay the base) and with its colour palette switched off; breakpoints are
  the device classes; cmdk 1.1.1 for the palette, loaded on demand; the React Compiler (babel-plugin-react-compiler
  1.0.0 through @rolldown/plugin-babel 0.2.4 and @babel/core 7.29.7); Vitest 5.0.3 with jsdom 30.1.2, Testing Library
  16.3.3 and axe-core 4.14.0; Inter and JetBrains Mono self-hosted from @fontsource-variable 5.3.0, Latin and Latin
  Extended only. Every version pinned exactly and published on or before 6 Oct 2026. Initial JavaScript 172 KB
  gzip (budget 250 KB). `make scan` allows SIL OFL 1.1 for `@fontsource*` packages only. **Left:** ESLint with the
  no-optimistic-money rule (3.4); AG Grid and TanStack Table and Virtual with the grid (3.1).)
- [x] 1.2 Write the feature registry and its test: a route or action without a mode tag, a help key or a guide page fails. Tests `// req: FR-UX-29` and `// req: FR-UX-31`
  (20 Oct 2026: `apps/web/src/registry.ts`, design.md's path (MASTER.md §6 and the two skills that named
  `src/ui/modes.ts` now name it too), lists routes, actions and navigation items with mode, help key and guide page; tooltip text is
  in `src/i18n/en-SG.json`, never in code; `registry.test.ts` fails on a missing mode, help key, help text, guide page
  or guide heading, and on a page the router serves that the registry does not list. New guide pages `home.md` and
  `appearance.md`, compiled into `kb/`. `make test` runs the web unit tests. **Left:** field groups and column sets
  join the registry with the grids and records (3.1, 3.2); tour steps (4.2).)
- [x] 1.3 Themes (light, dark, system, high contrast, forced colours), surfaces, the teal and violet rules, icons, numbers, density and motion. Tests `// req: experience/visual-design <title>` for each of the 13, and `// req: FR-UX-38`
  (20 Oct 2026: `src/appearance.ts` sets Light, Dark or System, high contrast (or `prefers-contrast`), Comfortable or
  Compact density and Reduce motion on `<html>` for tokens.css; `prefers-reduced-motion` and the organisation's Off
  always win. Forced colours keep the focus ring and control borders. The header's Appearance menu and the command
  palette switch them. `format/amount.ts` shows a decimal string with U+2212 or brackets and copies a plain ASCII
  decimal, never parsing it into a number (RND-DISPLAY). `components/ai/AiBadge.tsx` is the only place violet
  appears, and a test keeps it so. Tests: `appearance.test.ts`, `styles/visual-design.test.ts` (12 of the 13 titles),
  `format/amount.test.ts`, `shell/AppFrame.test.tsx`, `components/ai/AiBadge.test.tsx`. Provisional: the appearance
  choice is kept in the browser until the preferences operation exists (4.1). **Left:** "Illustration and signature
  moments" (the Home pulse card and teaching empty states, 3.2 and 6.1); reading the organisation's motion switch
  from its settings (the session does not carry it; it is On); Basic mode forcing Comfortable density (4.1).)

## 2. Navigation and workspaces

- [ ] 2.1 The sidebar, personal section, badges and record page anatomy, and workspaces cut to the person's rights from the session's effective rights. Tests `// req: FR-UX-40`, `// req: FR-UX-39` and `// req: experience/navigation <title>`; a hidden endpoint called directly returns 403
  (20 Oct 2026, in part: `shell/AppFrame.tsx` is the designed frame: sidebar, rail in medium, bottom bar of at most
  five with More in compact, header with search, appearance and sign out, a skip link and `main#main`.
  `shell/navigation.ts` cuts the registry's navigation items to the roles and feature switches `GET /v1/session`
  returns, and to the mode. Tests `navigation.test.ts` and `AppFrame.test.tsx` (`// req: FR-UX-40`). Only Home is
  registered: each module adds its item with its first screen. **Left:** the personal section and FR-UX-39's personal
  workspace, badges, the record page anatomy, the organisation switcher, the Finance | People switch, and the Go test
  that calls each hidden endpoint directly and expects 403.
  20 Oct 2026, later: `session.tsx` is the signed-in layout every signed-in page shares (a pathless route): it asks the
  API where the browser belongs once, keeps the page asked for as `return_to` when it sends a browser to sign in,
  shows a session without a second factor only "Confirm it's you" (SEC-01, rev 0.43), and gives each page the
  person, the organisation, the mode and the "Confirm it's you" dialog (`stepup.tsx`: a passkey, or signing in again;
  an action the API refuses with API-AUTHZ-05 runs once more after it, with the same Idempotency-Key). Settings sits at
  the sidebar's foot; the Settings page lists its pages from the registry's `settingsNav`, cut by role and mode, and in
  Basic says how many more Expert mode shows (FR-UX-26). An unknown address says so, with a link to Home.)
- [ ] 2.2 The command palette, global search in PostgreSQL under row-level security, and Inbox triage whose approve shortcut opens the confirmation. Tests `// req: FR-UX-01`, `// req: FR-UX-02` and `// req: FR-UX-03`
  (20 Oct 2026, in part: the Inbox lists the approval requests and the weekly review; a request opens on its own page,
  and only its confirmation decides (FR-UX-03). The command palette leaves out pages of one record. **Left:** global
  search, the Inbox's other groups as their modules arrive, and keyboard triage.)
- [ ] 2.3 Drill-down to the Trace panel, side peeks and split view, keyboard-first entry and the shortcut sheet. Tests `// req: FR-UX-04`, `// req: FR-UX-05` and `// req: FR-UX-06`

## 3. Grids, records and correctness

- [ ] 3.1 The grid (Community edition until the licence arrives) virtualised to 100,000 rows, and statement tables with TanStack. Tests `// req: FR-UX-07` and `// req: NFR-PERF-UI-02` (≥ 50 fps in Playwright tracing)
- [ ] 3.2 Plain and Accountant views, status chips, guided set-up with the sample company, and teaching empty states. Tests `// req: FR-UX-08`, `// req: FR-UX-09`, `// req: FR-UX-10` and `// req: FR-UX-11`
- [ ] 3.3 Favourites, recents and saved views shareable by link, comments with mentions, and no hard delete. Tests `// req: FR-UX-12`, `// req: FR-UX-13` and `// req: FR-UX-14`
- [ ] 3.4 The live-update feed with `If-Match` conflicts, clipboard fidelity, the polite status region, no optimistic money (lint rule) and no re-rounding. Tests `// req: FR-UX-19`, `// req: FR-UX-20`, `// req: FR-UX-21`, `// req: FR-UX-22` and `// req: RND-DISPLAY`, plus the in-app notification entry `// req: FR-UX-15` (P1 part)

## 4. Modes and help

- [ ] 4.1 The mode toggle stored server-side, presentation only, the disclosure of hidden fields, discoverable growth and defaults by role. Tests `// req: FR-UX-23` through `// req: FR-UX-27`, and `// req: experience/modes <title>` for each of the 13 unnumbered requirements, with a check that safety information shows in Basic mode
  (20 Oct 2026, in part: the header's Basic | Expert switch (`role="radiogroup"`, arrow keys) and the choice on
  Settings › Profile and security; `src/mode.ts` starts a person in their roles' default, the Owner winning
  (FR-UX-27), and sets `data-mode` on the device for the kit's `.fy-basic-only` / `.fy-expert-only`. Tests
  `mode.test.ts`, `AppFrame.test.tsx`. Provisional: the choice is kept in this browser, like the appearance, until the
  preferences operation exists. **Left:** storing it on the server, the Administrator's default per role, the
  disclosure of hidden fields and the mode-parity test.)

- [ ] 4.2 Tooltips, `/help` built from `docs/user-guide/`, context-aware help, the first-run tour, practice in the sample company, and the FR-UX-41 banner from the review register. Tests `// req: FR-UX-30`, `// req: FR-UX-32`, `// req: FR-UX-33` (guide half), `// req: FR-UX-34`, `// req: FR-UX-36` and `// req: FR-UX-41`
  (20 Oct 2026, in part: the FR-UX-41 banner, `components/ReviewBanner.tsx`, reads the review register at build time
  and shows on each Settings page whose screen it lists while the area is pending. Test `ReviewBanner.test.tsx`.
  **Left:** the banner on the other screens as they are built, `/help`, context-aware help, the tour and practice.)

## 5. Devices, accessibility and budgets

- [ ] 5.1 The three device classes and layouts from 320 px, portrait and landscape, with no horizontal scroll. Tests `// req: FR-UX-37` and `// req: experience/devices-and-motion <class> device class` at the five frames (QA-UI-07)
- [ ] 5.2 WCAG 2.2 AA with axe on every shell screen, keyboard and focus tests, focus not obscured, dragging alternatives and accessible authentication. Tests `// req: NFR-A11Y-01` through `// req: NFR-A11Y-05`
- [ ] 5.3 Externalised strings with per-user formats and the organisation's time zone, and the page-load, interaction and bundle budgets measured in CI. Tests `// req: NFR-L10N-01`, `// req: NFR-PERF-UI-01` and `// req: NFR-PERF-UI-03`

## 6. Screens

- [ ] 6.1 Build the 18 shell screens as designed. Each has help keys, a `docs/user-guide/` page compiled into `kb/`, and a tour step where it is on the first-run path
- [ ] 6.2 Finish with `make check`, `make test` and `make test-e2e`

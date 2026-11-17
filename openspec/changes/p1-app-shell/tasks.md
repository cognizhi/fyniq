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
  moments" (the Home pulse card and teaching empty states, 3.2 and 6.1); Basic mode forcing Comfortable density (4.1).
  17 Nov 2026: the organisation's motion switch is the setting `appearance.motion` (On by default; an Administrator
  turns it off, audited), carried by `GET /v1/session` to everyone and applied before any page is drawn;
  `e2e/motion.spec.ts` finds no animation or transition with it Off, with Reduce motion or with
  `prefers-reduced-motion`, and finds some with it On (`// req: QA-UI-07`, `// req: FR-UX-38`).)

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
  Basic says how many more Expert mode shows (FR-UX-26). An unknown address says so, with a link to Home.
  17 Nov 2026, with p1-launch-readiness 5.3: the sidebar follows §7.1's table of what each role sees (FR-UX-40: an
  area for a right in one of its capabilities; a right that only approves, or reads one reference list, gives none).
  A person with personal rights only gets the personal workspace (FR-UX-39), and lands on My claims; the
  Administrator lands on Settings, the Auditor and the Board viewer on Reports, the Company secretary and the DPO on
  Compliance (`landingOf`). A page outside the person's areas shows "This page isn't part of your access"
  (`shell/NotPartOfAccess.tsx`, `mayOpen`) with a way back, never part of the page. Tests `navigation.test.ts` (each
  role's areas, the union of two, landings) and the browser walkthroughs `journeys/walkthroughs.spec.ts`, which also
  read the hidden area's list through the API and expect 403. **Left:** badges, the record page anatomy, the
  organisation switcher, the Finance | People switch; the Approver's "Home: My department" and the Auditor's
  workspace.)
  17 Nov 2026, later: Home's **Your work** (`screens/HomeWork.tsx`) holds the rest of §7.1's "Home shows" for the
  Bookkeeper, the AR and AP specialists and the payment releaser: each row counts a list the role works through and
  opens it (bank lines to categorise, drafts to finish, overdue invoices and bills, receipts to apply, quotes waiting,
  bills to pay, batches being prepared or waiting for release, claims to pay, bank-detail changes waiting, payments
  the bank rejected) and the releaser's bank balances; a read the API refuses leaves its row out. **Upcoming
  deadlines** lists the calendar's next five, overdue and due soon first, for whoever may read it. Tests
  `HomeWork.test.tsx` and the walkthroughs' rows per role. The payroll administrator's rows (next pay run, salary
  batches, payroll postings) wait for payroll (after go-live); CPF shows under Upcoming deadlines.
- [ ] 2.2 The command palette, global search in PostgreSQL under row-level security, and Inbox triage whose approve shortcut opens the confirmation. Tests `// req: FR-UX-01`, `// req: FR-UX-02` and `// req: FR-UX-03`
  (20 Oct 2026, in part: the Inbox lists the approval requests and the weekly review; a request opens on its own page,
  and only its confirmation decides (FR-UX-03). The command palette leaves out pages of one record. **Left:** global
  search, the Inbox's other groups as their modules arrive, and keyboard triage.)
  (6 Nov 2026, PRD-001 rev 0.190: the global search. `GET /v1/search` (`searchRecords`, any member) reads the query
  (`internal/domain/search`: an amount exact or bounded, with k and m, or text; a bare whole number as both), finds
  documents by number, reference, memo, notes, title, lines and party and by an amount on a live posted line
  (`Documents.Search`), contacts (`Contacts.Search`), accounts and items, each kind by its own row (SEC-34), an
  Employee's own claims; up to 20 each with counts and "more", each result with its link. The Search page at `/search`
  (filters by kind, arrow keys between results) and the palette's "Search the records for …". Guide `search.md`, compiled
  into `shell.search`. Tests `TestTheGlobalSearchFindsWhatThePersonMaySee`, `TestAQueryReadsAsAnAmountOrText`,
  `search.test.tsx`, `AppFrame.test.tsx`, the matrix's row. **Left:** bank lines and help pages in the results, the
  Inbox's other groups and keyboard triage.)
  (6 Nov 2026, PRD-001 rev 0.192: the Inbox's keys. J or ↓, K or ↑, E or Enter, A, R and ?, only while a request has
  the focus (WCAG 2.2 SC 2.1.4); A and R open the approval page with its confirmation showing (`?decide=`), only for a
  person the request lets decide. Guide `approvals.md` (From the keyboard), compiled into `approvals.decide`. Tests
  `approvals.test.tsx` "the Inbox's keys". **Left:** bank lines and help pages in the search, the Inbox's other groups as
  their modules arrive, grouping by due date, and bulk actions where safe.)
- [ ] 2.3 Drill-down to the Trace panel, side peeks and split view, keyboard-first entry and the shortcut sheet. Tests `// req: FR-UX-04`, `// req: FR-UX-05` and `// req: FR-UX-06`
  (12 Nov 2026, in part: the Trace panel (`shell/Trace.tsx`, docs/design/screens TracePanel), opened from any account
  figure, group total or section total of a statement as a side sheet that keeps the report (the `trace` search
  parameter, so Back closes it): 1 the accounts a total is made of; 2 an account's ledger lines read as at the
  statement's revision (`getGeneralLedger`'s new `as_at_revision`, `balance.GeneralLedgerAt`; FR-RPT-08); 3 a line's
  entry and source document (`getLedgerEntry`: the entry, its lines, and its document's type, number, status, date,
  customer or supplier and page); 4 that document's history from the audit trail, for those whose roles read it.
  Guide `financial-statements.md` (Trace a figure to its source), compiled into `reports.statements`. Tests
  `TestALedgerEntryTracesToItsSource`, `TestTheGeneralLedgerAsAtARevision`, the matrix's row, `Trace.test.tsx`,
  `statements.test.tsx`. **Left:** the Trace panel from Home's tiles and the other reports; side peeks over lists and
  the split view for matching; keyboard-first entry and the shortcut sheet.)

## 3. Grids, records and correctness

- [ ] 3.1 The grid (Community edition until the licence arrives) virtualised to 100,000 rows, and statement tables with TanStack. Tests `// req: FR-UX-07` and `// req: NFR-PERF-UI-02` (≥ 50 fps in Playwright tracing)
- [ ] 3.2 Plain and Accountant views, status chips, guided set-up with the sample company, and teaching empty states. Tests `// req: FR-UX-08`, `// req: FR-UX-09`, `// req: FR-UX-10` and `// req: FR-UX-11`
  (20 Oct 2026, in part: Home's "Set up the company" checklist with progress, from what the API can tell (the profile's
  name, UEN and books' start, the chart, money accounts, contacts, a second member or an invitation, a passkey; a read
  the person's roles cannot make leaves its step out), and "Waiting for you" (approvals and the weekly review).
  Teaching empty states on the chart, contacts, items and review log. Test `Home.test.tsx`. **Left:** the sample
  company to explore, the checklist's later steps (opening balances, bank feeds, employees, the workbook import), the
  Plain and Accountant views.)
  (17 Nov 2026, for the launch gate: the Plain and Accountant views (FR-UX-08). `src/view.ts` keeps each person's
  choice (in this browser until task 4.1 stores preferences on the server) and gives Basic mode Plain view always;
  `components/LedgerViewToggle.tsx` offers it in Expert mode on the bank categorise grid, whose money shows as Money in
  / Money out / Suggested category or, as the money account's ledger has it, Debit / Credit / Suggested account, the
  same figures, and under Settings › Profile and security. Guide `banking.md`, compiled. Tests
  `LedgerViewToggle.test.tsx`, `categorise.test.tsx`. **Left:** the view on the grids still to come (journal lines,
  budgets, opening balances) as each is built.)
- [ ] 3.3 Favourites, recents and saved views shareable by link, comments with mentions, and no hard delete. Tests `// req: FR-UX-12`, `// req: FR-UX-13` and `// req: FR-UX-14`
  (17 Nov 2026, in part, for the launch gate: no hard delete (FR-UX-14). `TestEachRoleHoldsExactlyItsGrants` refuses a
  DELETE grant to any application role but on five tables named with why (housekeeping swept on its schedule, and an
  item's price list replaced with its audited version); TRUNCATE was refused already. Every DELETE operation of the API
  withdraws or marks (removed evidence is kept, a deleted conversation is marked until the purge). The screen half:
  a void is asked in a confirmation that needs its reason (`bills.test.tsx`). Bulk voids, with their typed
  confirmation, come with a bulk void. **Left:** favourites, recents, saved views and comments.)
- [ ] 3.4 The live-update feed with `If-Match` conflicts, clipboard fidelity, the polite status region, no optimistic money (lint rule) and no re-rounding. Tests `// req: FR-UX-19`, `// req: FR-UX-20`, `// req: FR-UX-21`, `// req: FR-UX-22` and `// req: RND-DISPLAY`, plus the in-app notification entry `// req: FR-UX-15` (P1 part)
  (6 Nov 2026, PRD-001 rev 0.193, clipboard fidelity: `installClipboard` (started with the app) rewrites a copy of text on
  screen so each displayed amount is a plain decimal with an ASCII minus (`clipboardText`), and reads a paste into any
  amount field (`inputMode="decimal"`) in the forms people copy and the browser's locale (`pastedAmount`), putting the
  plain decimal in the field before the form is saved; characters only, no JavaScript number. Guide `home.md` (Copy and
  paste amounts), compiled into `home.overview`. Tests `amount.test.ts`. **Left:** the live-update feed (FR-UX-19), one
  polite status region for the shell's counts (FR-UX-21), the lint rule against optimistic money (FR-UX-22).)
  (6 Nov 2026, PRD-001 rev 0.194, no optimistic money: the web app has no linter, so `noOptimisticMoney.test.ts` reads its
  sources and fails on a query-cache write that does not name the server's answer it stores ("the server's answer
  (FR-UX-22)" on its line) and on an `onMutate` that writes the cache. **Left:** the live-update feed (FR-UX-19) and one
  polite status region for the shell's counts (FR-UX-21).)

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
  (17 Nov 2026, the tour's server half, for the launch gate: `user_help_state` (migration 000441), append-only and
  audited, under row-level security and with the members' foreign key; `GET /v1/help-state` (offered until answered,
  offered again to resume where it stopped) and `PUT /v1/help-state/tour` (started at a step, finished or skipped;
  If-Match, Idempotency-Key, preview), each person's own (`HLP-01` for a caller that is not a person). Test
  `TestTheTourSavesOnlyItsOwnState`: every save writes the state and its audit event and nothing else (G4-104).
  **Left:** the tour itself in the web app and its end-to-end test per role (QA-UI-06).)
  (17 Nov 2026, the user guide, for the launch gate: `/help` builds every page of `docs/user-guide/` into the web app
  (`help/guide.ts`, at build time; the web image copies the guide and the release notes), lists those the person's
  roles call for, Basic ones in Basic mode with how many Expert adds, and searches them as they type; `/help/$key`
  renders a page's Markdown as elements (`help/markdown.tsx`: headings with their anchors, nested lists, tables,
  notes, code; a link to another page opens it, one to an outside site a new tab, any other its words), with its
  sections in a sticky contents column and a print without the shell; `/help/whats-new` is the release notes. Help ›
  User guide opens the page of the screen the person is on, from the registry's guide keys (FR-UX-33's guide half;
  `help/links.ts`, kept apart so the shell does not load the pages). Guide `help.md`, compiled into `help.guide`.
  Tests `help.test.tsx` (`// req: FR-UX-32`, `// req: FR-UX-33`). **Left:** the glossary and the keyboard shortcuts'
  pages, which wait for their sources; screenshots from the end-to-end tests; "Try it in the sample company"; the
  tour and practice.)

## 5. Devices, accessibility and budgets

- [x] 5.1 The three device classes and layouts from 320 px, portrait and landscape, with no horizontal scroll. Tests `// req: FR-UX-37` and `// req: experience/devices-and-motion <class> device class` at the five frames (QA-UI-07)
  (17 Nov 2026: `e2e/devices.spec.ts` opens each shell screen at the five reference frames, the 320 px small phone and
  four landscapes: no sideways page scroll, the class's own navigation (sidebar, rail, or the bottom bar of four and
  More) and the same safety information in every class; on a touch screen every control's hit area is 44 × 44 px;
  folding, unfolding and rotating keep the page, its filters, the scroll position and an unsaved draft; and on a screen
  that spans a hinge a list and its detail split at it (`shell/hinge.ts`, from the Viewport Segments API). Built for it:
  the compact header's mode pill, More always in the bottom bar with the account (appearance, About, sign out) the
  compact header has no room for, scroll padding that keeps a focused field clear of the header and the bottom bar,
  and 44 px targets on coarse pointers (app.css); twelve radios and checkboxes had the 18 px box's class as their
  label, now a row whose whole label is the target. **Left:** visual snapshots (QA-UI-07's hashes, p1-launch-readiness
  5.4); devices without the Viewport Segments API split by CSS alone.)
- [ ] 5.2 WCAG 2.2 AA with axe on every shell screen, keyboard and focus tests, focus not obscured, dragging alternatives and accessible authentication. Tests `// req: NFR-A11Y-01` through `// req: NFR-A11Y-05`
  (17 Nov 2026, in part, for the launch gate: NFR-A11Y-03 to 05. `e2e/focus.spec.ts` tabs through Home, the invoices,
  a statement and Settings at the desktop and phone frames, and through Home with the AI panel open: no focused control
  is hidden by the sticky header, the bottom bar or the AI panel (by what is drawn at its centre, so stacking counts)
  or by a toast (by covering it entirely); a planted toast must be caught. `draggingAlternatives.test.ts`: nothing is
  done only by dragging (a drop zone offers the file chooser; a drag that moves or resizes names its alternative, none
  yet). `accessibleAuthentication.test.tsx`: the way in leaves paste and password managers alone and asks no
  cognitive test, the step-up offers a passkey and a new sign-in, a pasted recovery code is taken, and a refused
  submit's summary takes focus. **Left:** NFR-A11Y-01 and 02's axe pass on every shell screen and keyboard tests; the
  sticky first column's scrolling when the grid is built.)
- [ ] 5.3 Externalised strings with per-user formats and the organisation's time zone, and the page-load, interaction and bundle budgets measured in CI. Tests `// req: NFR-L10N-01`, `// req: NFR-PERF-UI-01` and `// req: NFR-PERF-UI-03`

## 6. Screens

- [ ] 6.1 Build the 18 shell screens as designed. Each has help keys, a `docs/user-guide/` page compiled into `kb/`, and a tour step where it is on the first-run path
- [ ] 6.2 Finish with `make check`, `make test` and `make test-e2e`

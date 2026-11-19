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
  (20 Nov 2026, the badges: the Inbox (requests waiting for the person's approval), Sales (invoices overdue), Purchases (bills
  due), Banking (bank lines to categorise) and Compliance (statutory deadlines due) carry their count, each read from the
  list it opens by the API, which decides who may read it, and again every minute (`shell/badges.ts`); a screen reader hears
  it in words ("Inbox, 3 approvals waiting"). Guide `home.md` (The sidebar's counts), compiled into `home.overview`. Test
  `AppFrame.test.tsx`. **Left:** the badges of Accounting (the close status), Equity (ACRA tasks due), Projects (over
  budget) and Payroll (due status), with their modules' work; the Inbox's count of its other groups; the record page
  anatomy, the organisation switcher, the Finance | People switch, the Approver's My department and the Auditor's workspace.)
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
  (17 Nov 2026, for the launch gate: keyboard-first entry (FR-UX-06; docs/design/screens Shortcuts). `shortcuts.ts`:
  "G then" a letter to each sidebar area the person sees and "C then" a letter for every create page their access
  opens (a test fails a create page without keys), Expert mode only; ? opens the shortcut sheet in both modes, as do the
  command palette and Settings › Profile and security › Keyboard. None fires while focus is in a field or a dialog is
  open, or on a key the page handled (the Inbox's own). Each person turns the single-character shortcuts off or changes
  their keys, in the sheet and on the profile page, with clashes refused field by field (SC 2.1.4); kept in this
  browser under the person's user ID until task 4.1's preferences operation stores it with the mode. Enter moves to the
  next field and Ctrl+Enter saves in the invoice, quote, bill, purchase order, journal, recurring and claim editors
  (`data-enter="next"`). A page's actions on its selected rows join the command palette (`shell/bulk.ts`; Categorise's
  grid offers "Accept the selected suggestions"). Guide `keyboard-shortcuts.md` compiled into `shell.shortcuts`, and
  `profile-and-security.md`'s Keyboard section. Tests `shell/keyboard.test.tsx`, `categorise.test.tsx`. **Left:** the
  shortcut stored server-side with the mode (task 4.1); the mode-toggle shortcut (FR-UX-23).)

  (20 Nov 2026, PRD-001 rev 0.326: the shortcut sheet's on switch and each person's keys are kept on the server with
  the other preferences, and the mode-toggle shortcut (Alt+Shift+M, FR-UX-23) is built. **Left:** side peeks and the
  split view, the Trace panel from Home's tiles and the other reports.)
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
  (20 Nov 2026, PRD-001 rev 0.326: the Plain or Accountant view is kept on the server with the other preferences.
  **Left:** the sample company to explore, the checklist's later steps, the view on the grids still to come.)
- [ ] 3.3 Favourites, recents and saved views shareable by link, comments with mentions, and no hard delete. Tests `// req: FR-UX-12`, `// req: FR-UX-13` and `// req: FR-UX-14`
  (17 Nov 2026, in part, for the launch gate: no hard delete (FR-UX-14). `TestEachRoleHoldsExactlyItsGrants` refuses a
  DELETE grant to any application role but on five tables named with why (housekeeping swept on its schedule, and an
  item's price list replaced with its audited version); TRUNCATE was refused already. Every DELETE operation of the API
  withdraws or marks (removed evidence is kept, a deleted conversation is marked until the purge). The screen half:
  a void is asked in a confirmation that needs its reason (`bills.test.tsx`). Bulk voids, with their typed
  confirmation, come with a bulk void. **Left:** favourites, recents, saved views and comments.)
  (20 Nov 2026, PRD-001 rev 0.325, FR-UX-12: favourites and saved views. `user_saved_item` (migration 000701; append-only, audited, per member,
  under row-level security), `GET`/`POST /v1/saved-items`, `DELETE /v1/saved-items/{kind}`; the header's star and the More sheet's
  Favourites and views dialog pin a page, save it with the filters its address carries, open and take away. A view is an
  address, so its link shares no data; the server derives the page's Appendix D area from its path and lists an item only while
  the roles hold a right there (SEC-34). Recent items stay in the browser (audit rule), owner to confirm. Guide `home.md`,
  compiled into `home.overview`. Tests `TestFavouritesAndViewsAreTheirOwnersOnly`, `TestAttackSavedItemsKeepTheirHistoryAndTheirTenant`,
  `saveditem_test.go`, `SavedMenu.test.tsx`. **Left:** FR-UX-13's comments and @mentions (a comment on any record needs each
  mentioned person's right to see it, the record kinds' visibility map, notifications and a panel on each record page) and
  moving the lists' filters into their addresses so a view keeps them.)
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
  (18 Nov 2026, G4-077 for the launch gate: `src/freshness.ts`, started with the app: a page back after being hidden,
  offline or asleep (its timers stopped) for longer than the feed window, 5 minutes [proposed], reloads its state
  once: every query re-reads through the API, which lists only what the person may still see, and each list is
  replaced, so nothing shows twice. Test `freshness.test.tsx`. **Left:** FR-UX-19's change feed itself (S), with
  its resume cursor, which this reload stands in for until it is built.)

  (20 Nov 2026, FR-UX-21: badge and status changes go through one polite, atomic status region (`shell/status.tsx`, in the app
  frame), as contextual phrases ("3 approvals waiting", "No approvals waiting", "2 unread notifications"), never bare
  numbers and never moving focus; the first count a page loads with is not announced, and the bell and the operations
  tray say theirs through it instead of each having a region. Test `AppFrame.test.tsx`. FR-UX-15's in-app, email and daily
  digest half is the notification centre of FR-NTF-01 to 05, tagged on its tests. **Left:** FR-UX-19's change feed, whose
  transport, server-sent events or polling, is chosen by a measurement under NFR-COST-02 that needs the cloud
  (`[live-verify]`); the page reload of `freshness.ts` stands in for it.)
## 4. Modes and help

- [ ] 4.1 The mode toggle stored server-side, presentation only, the disclosure of hidden fields, discoverable growth and defaults by role. Tests `// req: FR-UX-23` through `// req: FR-UX-27`, and `// req: experience/modes <title>` for each of the 13 unnumbered requirements, with a check that safety information shows in Basic mode
  (20 Oct 2026, in part: the header's Basic | Expert switch (`role="radiogroup"`, arrow keys) and the choice on
  Settings › Profile and security; `src/mode.ts` starts a person in their roles' default, the Owner winning
  (FR-UX-27), and sets `data-mode` on the device for the kit's `.fy-basic-only` / `.fy-expert-only`. Tests
  `mode.test.ts`, `AppFrame.test.tsx`. Provisional: the choice is kept in this browser, like the appearance, until the
  preferences operation exists. **Left:** storing it on the server, the Administrator's default per role, the
  disclosure of hidden fields and the mode-parity test.)

  (17 Nov 2026, for the launch gate: nothing silently hidden on records (FR-UX-25). `components/AdvancedDetails.tsx`:
  in Basic mode, "Has advanced details (N)" lists read-only the values a record has in fields only Expert shows, with
  "Open in Expert mode"; nothing in Expert mode. The invoice, credit note and recurring invoice lines (discount, spread,
  dimension values), the bill lines (spread, dimension values) and the claim's receipts (dimension values) use it: such
  a field is Expert's in Basic now even with a value, unless its account or category requires it or it carries an
  error to fix. Guides `invoices.md`, `bills.md`, `claims-my-claims.md`, compiled. Tests `AdvancedDetails.test.tsx`,
  `invoices.test.tsx`.)
  (20 Nov 2026, PRD-001 rev 0.326, the server half: `user_preference` (migration 000700; append-only, audited, under
  row-level security and with the members' foreign key) keeps each person's mode, Plain or Accountant view, theme,
  contrast, density, motion, shortcuts' switch and keys, number format, date format and way of writing negatives, the
  same on every device. `GET` and `PATCH /v1/preferences` (`getMyPreferences`, `setMyPreferences`; If-Match,
  Idempotency-Key, preview; a save names only what it sets and what it resets) and the session carries them with the
  mode the person starts in and the organisation's time zone, so the first page is drawn as chosen. The Administrator's
  default per role (FR-UX-27) is the organisation setting `experience.default_mode.<role>` (Basic or Expert, shipped as the
  PRD's table), the Owner's winning. The mode switches from the command palette ("Switch to Expert mode") and with
  Alt+Shift+M, a chord that works in both modes. `preferences.ts` adopts the session's preferences and sends each later
  choice one after another, once more after a stale save; the browser's own copy only draws the sign-in page and the
  first paint, and a choice made in a browser before the server kept any is sent once. Tests `TestPreferencesAreKeptOnTheServer`,
  `TestAttackPreferencesKeepTheirHistoryAndTheirTenant`, `TestAStartingModeFollowsTheRolesAndTheOwnerWins`,
  `preferences.test.tsx`, `AppFrame.test.tsx` (the palette and the chord). **Left:** the disclosure of hidden fields'
  other half (FR-UX-25 on the lists, banking and reports), and the mode-parity test with the 13 unnumbered
  `experience/modes` requirements.)
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
  (18 Nov 2026, the tour in the web app: `tour/Tour.tsx` is MASTER.md §12's walkthrough (a non-modal spotlight and
  caption card on the real interface, focus to its title, → and ← and the buttons, Esc ends it, each step announced,
  focus back where it was; no movement with reduced motion); `tour/steps.ts` a variant per role group (founder,
  accountant, employee, approver, board viewer), at most seven steps on elements marked `data-tour`, a hidden one
  skipped; `tour/TourHost.tsx` offers it where the person lands until they answer, offers to resume one left part-way,
  runs it on Home for those who have it, and saves each step, finished or skipped, its only write. Help › Take the tour
  again replays it. Guide `help.md` (Take the tour), compiled. Tests `tour.test.tsx`, `e2e/tour.spec.ts`.)
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
  (18 Nov 2026, the ⓘ and the glossary, for the launch gate: `components/HelpPopover.tsx` is MASTER.md §12's info
  popover (a 24 × 24 px ⓘ beside the visible label, or a glossary term's own words; opens on hover and focus, pins on a
  click or tap, stays while hovered, Esc closes it and returns focus, an outside click closes a pinned one; a
  non-modal dialog named by its title, with "Learn more"). `help/HelpInfo.tsx` fills it from a help key, whose
  catalogue entry may now carry `detail`, `example` and the `expert` detail Expert mode adds; `help/Term.tsx` from
  `docs/user-guide/glossary.md` (20 terms, draft until the accountant reviews them). The KPI tiles' ⓘ use it; Books
  health has one; the suspense note's terms are glossary terms. Guide `glossary.md`, compiled into
  `reference.glossary`. Tests `HelpPopover.test.tsx`, `glossary.test.tsx`, `kpis.test.tsx`. **Left:** FR-UX-30's
  breadth: help on every field label, column header, status chip, stat tile and chart across the screens (most have a
  help key's native tooltip only), screen by screen.)
  (18 Nov 2026, FR-UX-11's rest, for the launch gate: "What happened? Where to record it" on Home
  (`screens/WhatHappened.tsx`, from the workbook's Start Here: nine things that happen, each with the page that records
  it and what to do there, listed only when the person's access opens that page). Every empty list explains what goes
  there and offers its first action: rates, dimensions, payment batches, items, contacts, pre-incorporation costs and
  bank rules now offer "Add your first …" in the empty state, the fixed-asset register what is not in it yet, and
  Categorise importing a statement; those filled from elsewhere are listed with why. Guide `home.md`, compiled. Tests
  `whatHappened.test.tsx`.)

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
- [x] 5.2 WCAG 2.2 AA with axe on every shell screen, keyboard and focus tests, focus not obscured, dragging alternatives and accessible authentication. Tests `// req: NFR-A11Y-01` through `// req: NFR-A11Y-05`
  (17 Nov 2026, in part, for the launch gate: NFR-A11Y-03 to 05. `e2e/focus.spec.ts` tabs through Home, the invoices,
  a statement and Settings at the desktop and phone frames, and through Home with the AI panel open: no focused control
  is hidden by the sticky header, the bottom bar or the AI panel (by what is drawn at its centre, so stacking counts)
  or by a toast (by covering it entirely); a planted toast must be caught. `draggingAlternatives.test.ts`: nothing is
  done only by dragging (a drop zone offers the file chooser; a drag that moves or resizes names its alternative, none
  yet). `accessibleAuthentication.test.tsx`: the way in leaves paste and password managers alone and asks no
  cognitive test, the step-up offers a passkey and a new sign-in, a pasted recovery code is taken, and a refused
  submit's summary takes focus. **Left:** NFR-A11Y-01 and 02's axe pass on every shell screen and keyboard tests; the
  sticky first column's scrolling when the grid is built.)
  (20 Nov 2026, the rest: `e2e/accessibility.spec.ts` runs axe-core's WCAG 2.0 to 2.2 A and AA rules and its best practices on each of
  the 12 shell screens as drawn in Chrome, at the desktop frame in light, dark and high-contrast themes and at the phone
  frame (motion reduced, as a person who asks for it has it: a fade caught half-way is a colour no one sees at rest), and
  tabs through each screen: one main, the main navigation and banner, the skip link first on a fresh page and going to the
  main content, and every control that takes focus with an accessible name and a visible focus indicator. All 60 pass.
  **Left:** the sticky first column's scrolling comes with the grid (3.1).)
- [ ] 5.3 Externalised strings with per-user formats and the organisation's time zone, and the page-load, interaction and bundle budgets measured in CI. Tests `// req: NFR-L10N-01`, `// req: NFR-PERF-UI-01` and `// req: NFR-PERF-UI-03`

  (20 Nov 2026, PRD-001 rev 0.326, NFR-L10N-01: each person's number format (1,234.56, 1.234,56, 1 234,56, 1 234.56), date
  format (12 Oct 2026, 12/10/2026, 10/12/2026, 2026-10-12) and way of writing negatives are chosen in Settings › Profile
  and security and kept on the server; every date and time is written in the organisation's time zone, which the
  session carries (`format/locale.ts`: only characters move, and a calendar day with no time is never moved by a zone);
  copying and pasting amounts follow the format (`clipboardText`, `pastedAmount`); the help catalogue is the externalised
  strings (`i18n/en-SG.json`). Tests `preferences.test.tsx`, `Profile.test.tsx`, `date.test.ts`. **Left:** the screens
  still writing a date or count with a fixed `en-SG` (the sign-in and install pages, which have no session, and a few
  weekday and month names), and NFR-PERF-UI-03's initial bundle.)
  (20 Nov 2026, the bundle: Home's dashboard, the roles' work, the policies to acknowledge and what-happened cards load after its first
  paint (lazy, `screens/Home.tsx`), which took the initial JavaScript from 337 to 292 KB gzip. The rest is the framework
  (react-dom, Base UI and TanStack, 147 KB), the help catalogue (33) and the registry (25), icons (22) and the shell's own code:
  250 KB is out of reach without splitting the catalogue per area and the registry per module, which this change does not do. **An
  exception for the owner to confirm:** the measured gap is 42 KB over the PRD's 250 KB; `e2e/bundle.spec.ts` holds a ceiling
  of 295 KB. The budget is not raised.)
## 6. Screens

  (18 Nov 2026, the page-load and interaction budgets, for the launch gate (NFR-PERF-UI-01): `e2e/performance.spec.ts`,
  in Chrome over an emulated 4G link (9 Mbps down, 1.5 Mbps up, 85 ms) on the built app with fixture answers: the
  75th percentile Largest Contentful Paint of eight cold loads of Home, the invoices, the Inbox and Settings under 1.5 s,
  the slowest interaction's next paint under 200 ms (the mode switch, the palette, the Help menu), and a change of route
  whose data is cached under 300 ms; it logs what it measured (locally, at half the CPU: 1.3 s, 48 ms, 35 ms). **Left:**
  NFR-L10N-01's externalised strings and per-user formats.
  (20 Nov 2026, the bundle budget measured (NFR-PERF-UI-03): `e2e/bundle.spec.ts` gzips what index.html loads before the
  first paint: 333 KB, 83 KB over the PRD's 250 KB (the 172 KB of 20 Oct has grown with the screens): index 96, framework
  147 (react-dom, Base UI, TanStack), the help catalogue 34, icons 22, the registry 17. The test holds a ceiling of 340 KB
  so it cannot grow, and checks the command palette's library stays out of the initial load. **Left:** lower it to 250 KB
  by loading the help catalogue on demand, Trace, Recovery and the report tiles' code lazily, then tag NFR-PERF-UI-03.)
- [ ] 6.1 Build the 18 shell screens as designed. Each has help keys, a `docs/user-guide/` page compiled into `kb/`, and a tour step where it is on the first-run path
- [ ] 6.2 Finish with `make check`, `make test` and `make test-e2e`

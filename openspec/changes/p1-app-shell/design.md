# Design

## Context

- `apps/web` is React 19 and Vite 8, with `openapi-fetch` on the generated client and Lucide.
- The design tokens live in `docs/design-system/fyniq/tokens.css`, and `make check` validates their
  contrast in four themes.
- The screen fragments in `docs/design/screens/` are the visual reference, at five frames, light and
  dark.
- `docs/prd/reviews/review-status.json` lists the pages that show the FR-UX-41 banner.
- ADR-T1 (accepted) chose the libraries.

## Goals / Non-Goals

**Goals:**
- A later screen declares its route, registry entry, help key and required rights, and gets
  navigation, modes, help, themes, device layouts and accessibility from the shell.

**Non-Goals:**
- Each module's own screens.
- The AI assistant's answers (wave 6).
- Slack and other channels (P2).

## Decisions

- **One feature registry drives everything.**
  - `apps/web/src/registry.ts` lists each route, navigation item, field group, column set and action,
    with:
    - its mode (`basic` or `expert`);
    - its help key, tooltip, guide page and tour step;
    - the matrix area and right it needs.
  - The sidebar, palette, search, mode filter, help panel and tour all read it.
  - A test fails when a route or action lacks a mode tag, a help key or a guide page (FR-UX-29,
    FR-UX-31, QA-UI-05).
- **Rights come from the server.**
  - The session operation returns the person's effective rights from the matrix (`p1-users-roles`),
    and the shell hides areas without a right (FR-UX-40).
  - The server still enforces every right. A test calls each hidden endpoint directly and expects
    403.
- **Money renders only from server data.**
  - TanStack Query mutations for ledger figures invalidate and refetch, and never set cache data
    optimistically. A lint rule, with ESLint added alongside the libraries, bans `setQueryData` on money queries (FR-UX-22).
  - Formatting uses the stored decimal string and never re-rounds (RND-DISPLAY). The clipboard writes
    an ASCII minus and plain decimals (FR-UX-20).
- **Live updates through a server-sent event feed.**
  - The API streams record and list versions for the person's scope (SEC-34). The shell invalidates
    matching queries, and edits carry `If-Match`, so a conflict shows edit-conflict.
  - The stream is served by the API on request, with no extra service (ADR-T7).
- **Grid.**
  - AG Grid for data entry and large ledgers, loaded on demand (NFR-PERF-UI-03).
  - TanStack Table and Virtual for statements, as semantic HTML tables.
  - Enterprise features switch on when the licence key is present.
- **Search in PostgreSQL.** Full-text search over documents, contacts, accounts, references and notes,
  with the person's row scope applied by the same row-level security. No external search service in
  P1.
- **Help content.** The guide pages build into the web app from `docs/user-guide/`. The help registry
  points at their anchors, and the knowledge base in `kb/` compiles from the same pages
  (`fyniq-knowledge-base`).
- **Ports and adapters (ADR-T8):** storage for preferences, saved views, comments and search; queue
  for mention notifications. No new port.
- **Audit events (AUD-01):** comments and mentions, saved-view sharing, and changes to the
  organisation's motion setting and default modes by role. Personal preferences change only the
  person's own state and are audited as preference changes.

## Decisions taken while building (20 Nov 2026, the owner away: the recommended option each time)

- **Preferences are one append-only row per save, per member and organisation** (`user_preference`, migration 000700), read with the
  session and saved with `PATCH /v1/preferences` (partial, `reset` takes a field back to its default, If-Match). Rejected: a row per
  preference (many tiny writes, no atomic state) and a document in `localStorage` only (the same mode on every device is FR-UX-23).
  The browser keeps a copy to draw the sign-in page and the first paint, and uploads a choice it holds that the server lacks, once.
- **The Administrator's default per role is one organisation setting per role** (`experience.default_mode.<role>`), not under dual
  control: it changes only what is shown, never a right. The Owner's setting wins for a person who holds the Owner role.
- **The mode switches from the keyboard with Alt+Shift+M**, a chord with modifiers, so SC 2.1.4's single-character rule does not apply
  and it works in both modes and inside a field.
- **Number and date formats are four choices each, and negatives two**, not free patterns: each is testable end to end (display, copy,
  paste), and a free pattern could re-round an amount. The time zone is the organisation's, never the person's.
- **One polite, atomic status region for the shell's counts** (`shell/status.tsx`); sources say a phrase, the first count of a page is
  not announced. Badges are counted from the list each opens, by the API, so they follow the person's rights.
- **FR-UX-19's transport, when built: polling the change feed, not server-sent events.** Under NFR-COST-02 a held-open request keeps
  an instance billed while a tab is visible; a cursor read every 20 seconds while visible, stopped after 5 minutes hidden, costs only
  while a person is working and works through every proxy. The feed and its cursor are the same either way, so a measured switch to
  server-sent events changes the web app's reader only. Not built in this change's slices: it needs a measurement in the cloud
  (`[live-verify]`).

- **The entry grid is our own, not AG Grid Community** (`components/EntryGrid.tsx`, PRD-001 rev 0.383, MASTER.md's "AG Grid (entry grids)" changed,
  owner to confirm). FR-UX-07 asks for range selection, fill down, multi-cell edit and a paste from Excel; in AG Grid those are Enterprise features,
  so the Community edition would have meant building them on top of a large dependency, over a bundle that is above its budget already. The grid draws
  only the rows in view (a fixed row height and a scroll position), is an ARIA grid with one tab stop, and keeps amounts as decimal strings: a sum
  with `=` is worked out on scaled integers, and a division only when it is exact, since a rounded quotient needs a named rounding policy a cell cannot
  name. Statement tables stay with TanStack Table, as MASTER.md has them.

## Risks / Trade-offs

- [The 250 KB initial-JavaScript budget against a heavy grid and component kit] → The grid, charts
  and PDF preview load on demand and are preloaded on intent. CI measures the shell bundle and fails
  over budget.
- [Eighteen screens at five frames is a lot of visual surface] → Playwright snapshots run at the five
  frames in light and dark for the shell screens, and per-screen device tests run at the three device
  classes (QA-UI-07).

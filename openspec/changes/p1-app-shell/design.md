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

## Risks / Trade-offs

- [The 250 KB initial-JavaScript budget against a heavy grid and component kit] → The grid, charts
  and PDF preview load on demand and are preloaded on intent. CI measures the shell bundle and fails
  over budget.
- [Eighteen screens at five frames is a lot of visual surface] → Playwright snapshots run at the five
  frames in light and dark for the shell screens, and per-screen device tests run at the three device
  classes (QA-UI-07).

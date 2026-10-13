---
name: fyniq-design-system
description: Build and review the user interface of Fyniq with its design system — tokens (light, dark, high contrast), the brand-teal vs AI-violet rule, Lucide icons, status chips, density, Basic and Expert experience modes, tables and grids, charts, numbers and WCAG 2.2 accessibility — and consult the uipro (ui-ux-pro-max) skill as a review lens. Use this for any change under apps/web or docs/design-system, any new page, component, form, chart, dashboard tile, theme or colour, any Basic/Expert mode question, and whenever someone says "make it look better", "design this screen", "add dark mode", "is this accessible", "which icon", "add a chart", "hide this for beginners", "review the UI" or "use uipro".
---

# Designing and building the UI

The requirements are in PRD-001 §7 (rev 0.2). How they look and behave is in the design system:

| File | Role |
|---|---|
| `docs/design-system/fyniq/MASTER.md` | Global rules: direction, colour roles, type, numbers, layout and density, modes, components, icons, motion, charts, accessibility, anti-patterns |
| `docs/design-system/fyniq/pages/<page>.md` | Page overrides (`home`, `inbox`, `banking-categorise`, `reports`). **A page file wins over MASTER for that page**; anything it does not mention follows MASTER |
| `docs/design-system/fyniq/tokens.css` | OKLCH semantic tokens for `:root`, `.dark`, `[data-contrast="more"]`, `.dark[data-contrast="more"]`, mapped to Tailwind 4 with `@theme inline` |
| `docs/design-system/fyniq/scripts/check_contrast.py` | Contrast validator for every required token pair in all four themes (QA-UI-03); `make check-tokens` |
| `docs/design/screens/` | **The visual reference** (rev 0.10): every screen in `inventory.json`, as fragments in `src/` built by `build.py` onto Claude Design canvases (one per area, URLs in `canvases.json`) at five frames in light and dark. `AUTHORING.md` is the how-to; `kit/ui.css` the component layer. Recipe: **fyniq-openspec** `references/design.md` |
| `docs/design/prototype/index.html` | Superseded early prototype (shell, Home, Inbox, Banking, Reports); kept for its tour and popover demos |

From P0 the web app (`apps/web`) imports `tokens.css`; until then the screen kit is generated from it.
When you change a token, change `tokens.css` first, run `make check-tokens`, then `make design-kit`
and republish the canvases and the Fyniq design system (fyniq-openspec `references/design.md`).

## Order of work for any screen

1. Find the requirement IDs (§7, §8) and the page override, if any.
2. Decide what Basic and Expert show (§7.8 table). Tag everything in the feature registry.
3. Lay out with tokens only: surfaces, spacing (4 px grid), density, one primary action.
4. Choose components from MASTER §7 (shadcn/ui on Base UI); AG Grid for entry grids,
   TanStack Table for statements; ECharts for charts.
5. Write the states: loading (skeleton after 300 ms), empty (teaching), error (with recovery),
   permission-denied, locked period.
6. Accessibility pass (below), both themes, high contrast, forced colours, and every device class
   down to the 344 px folded foldable.
6a. Design it on the canvas first (fyniq-openspec `references/design.md`) and get it reviewed there.
7. Tests: component (Vitest), story with axe (Storybook), visual snapshot in both themes,
   mode-parity test (QA-UI-01…04).

## The colour rules that matter most

| Rule | Why |
|---|---|
| **Brand teal** (`--primary`, `--link`, `--ring`) for primary actions, links, selection and focus | One accent for "do this" |
| **AI violet** (`--ai`, `--ai-subtle`, `--ai-border`) **only** for AI-originated content, always with the "AI" badge (Lucide `sparkles` + "AI") | Users must never mistake AI content for human or posted data (REV-29). Violet on a button is a review blocker |
| `--ai-gradient` only on the Home pulse card's top band and the AI panel header | Gradients behind text break contrast |
| Status colours only for status, always icon + label | Colour never carries meaning alone (FR-UX-09, NFR-A11Y) |
| No raw hex or Tailwind palette classes (`bg-blue-500`) in components | Themes and the validator only work on tokens |
| Chart colours (`--chart-1…8`) are the dataviz reference palette; never for text or chrome | FR-VIZ-03/07/08 |

## Icons and status chips

**Lucide only** (shadcn/ui's components already use it; uipro's default Phosphor would make two
sets). Sizes 16/20/24 px, 1.5 px stroke. Decorative icons beside text get `aria-hidden="true"`;
icon-only buttons get an accessible name and a tooltip.

| Workbook symbol | Meaning | UI chip (icon · token) |
|---|---|---|
| ✔ | OK | `circle-check` · `--status-good` |
| ⚠ | Included, please review | `triangle-alert` · `--status-warning` |
| ✖ | Cannot post until fixed | `circle-x` · `--status-critical` |
| ⏰ | Due or overdue | `clock-alert` · `--status-serious` |
| ✎ | Draft | `pencil-line` · `--status-neutral` |
| 🔒 | Locked period | `lock` · `--status-neutral` |

The symbols themselves appear only in exports, CSV and plain-text email (FR-UX-09, rev 0.2).

## Basic and Expert modes (§7.8, FR-UX-23…29)

- **Presentation only.** The mode never changes permissions, validation, posting, approvals or
  the API request an action sends (FR-UX-24). Permissions come from roles; the mode only decides
  what is shown.
- **Never hidden in Basic:** ✖/⚠ statuses, approvals waiting, locked periods, compliance
  deadlines, AI labels and books health.
- **Feature registry:** every route, nav item, field group, column set and action is tagged
  `basic` or `expert` in `apps/web/src/ui/modes.ts` (P1). Untagged items fail a test
  (FR-UX-29). Use the registry, never `if (mode === …)` scattered in components.
- **Advanced-details disclosure:** if a record has values in fields Basic hides, show "Has
  advanced details (N) ▸", which reveals them read-only, with "Open in Expert mode" (FR-UX-25).
- **Discoverable:** ⌘K finds Expert features in Basic mode, tags them "Expert" and offers to
  switch (FR-UX-26). Hints at most once per session, dismissible.
- **Switching** keeps the page, filters and unsaved drafts. The header shows the segmented
  control ("Basic | Expert", `role="radiogroup"`).
- Defaults by role: FR-UX-27. FR-UX-08 (Plain/Accountant view) is a separate toggle inside
  Expert; Basic always uses the Plain view.
- Test: QA-UI-04 asserts identical API requests for the same action in both modes, and that the
  safety information is visible in both.

## Help, tooltips and the tour (§7.9, FR-UX-30…36; rev 0.3)

- **Help supplements labels; it never replaces them.** Short hints are tooltips; anything with
  an example or a link is an ⓘ info popover (≥ 24 × 24 px target, `aria-expanded`, opens on
  click/Enter, hover and focus; Esc closes; hoverable; persistent: WCAG 1.4.13).
- **One registry:** each field, column, chip, tile and route has a help key next to its
  `basic`/`expert` tag in `src/ui/modes.ts`; the key names the tooltip text, the guide page in
  `docs/user-guide/` (front-matter `key`) and the tour step. QA-UI-05 fails on a missing key.
- **Words:** Basic text is plain; Expert text adds the account, posting or rule ID. Use the UI's
  own names; one idea per tooltip.
- **First-run tour:** scrim + spotlight + caption card, ≤ 7 steps, Back/Next/Skip, Esc closes,
  focus moves into the card and returns afterwards, each step announced (`aria-live`), the
  spotlight never hides the focused element (2.4.11). It never changes data. Brand-teal ring,
  not AI violet. With `prefers-reduced-motion` the steps jump without animation. Progress is
  stored per user on the server; "Take the tour again" lives in Help.
- Patterns and tokens: MASTER.md "Help and onboarding"; the `/help` page: `pages/help.md`.

## Numbers and money in the UI

- Tabular figures (`font-variant-numeric: tabular-nums`) in every table, total, tile and axis;
  right-aligned; currency in the column header.
- Display negatives as "−1,234.56" (U+2212) or "(1,234.56)" per user preference. **Clipboard and
  exports use an ASCII "-" and plain decimals**; paste accepts `(1,234.56)`, `S$1,234.56`,
  `−1,234.56` and the user's locale, with a preview before commit (FR-UX-20).
- Money arrives as decimal strings with a currency (ENG-12). Never parse into a JS `number` for
  arithmetic; format strings, and use a decimal library for any client-side preview maths.
- **No optimistic money** (FR-UX-22): ledger-derived figures render only from server responses.
- Never re-round in the UI; totals come from the server (RND-DISPLAY).

## Density, layout, motion

Comfortable 40 px rows (default) and Compact 32 px (`[data-density="compact"]`); 56 px header;
240/64 px sidebar; 420 px side-peek and AI panel; pointer targets ≥ 24 × 24 px, touch ≥ 44 × 44 px.

**Device classes** (PRD §7.10, FR-UX-37; MASTER §5.1), mobile-first: **compact** < 600 px (top bar +
bottom bar of five; one column; tables become cards; panels full screen), **medium** 600–1023 px
(navigation rail; list and detail side by side; panels as sheets), **expanded** ≥ 1024 px (sidebar,
collapsed below 1280; docked panels). Design and review every screen at the five reference frames:
desktop 1440 × 900, tablet 834 × 1194, foldable open 690 × 840, phone 390 × 844, foldable folded
344 × 882 (the narrowest target). On a hinge-spanning device two panes split at the hinge.

**Motion** (FR-UX-38; MASTER §9): only the catalogue (page enter, panel and sheet, dialog, list
stagger, skeleton, status change, pulse ticker, books closed), 120/180/240 ms on `transform` and
`opacity`. The Administrator's organisation switch (`[data-motion="off"]`), the user's Reduce
motion (`[data-motion="reduce"]`) and `prefers-reduced-motion` all render final states at once.

## Accessibility checklist (WCAG 2.2 AA; NFR-A11Y-01…05)

- [ ] Contrast from tokens only; `make check-tokens` green (4.5:1 text, 3:1 boundaries; 7:1 text in high contrast)
- [ ] Every control reachable and operable by keyboard; visible 2 px focus ring; skip link; focus moves to the main region on route change
- [ ] **Focus not obscured** by sticky headers or columns, side-peek, AI panel, toasts or sticky action bars (SC 2.4.11): use `scroll-padding`
- [ ] Single-key shortcuts can be turned off and remapped, and never fire in text fields (SC 2.1.4; FR-UX-06)
- [ ] Every drag (fill handle, resize, tile layout) has a click or keyboard alternative (SC 2.5.7)
- [ ] Sign-in and step-up allow password managers, paste and passkeys (SC 3.3.8)
- [ ] Forms: visible labels, helper text, errors under fields via `aria-describedby`, error summary focused after a failed submit
- [ ] Live counts: one polite `role="status"` region with contextual phrases ("3 approvals waiting") (FR-UX-21)
- [ ] Charts: text summary, table view, keyboard-reachable tooltips
- [ ] Forced-colours mode keeps focus and boundaries visible
- [ ] axe reports 0 violations in Storybook and in the Playwright journeys

## Charts (§7.5 and the dataviz method)

Pick the form first (stat tile for a single headline; line for time; bar for comparison;
waterfall for bridges, ≤ 12 bars). One y-axis per chart. Categorical colours in fixed order
(`--chart-1…8`), following the entity, never its rank; more than 8 series fold into "Other".
Sequential = one hue; diverging = blue ↔ red with a grey midpoint; status colours reserved. A
legend for ≥ 2 series and direct labels for ≤ 4. Hover and focus tooltips; table view; PNG and
XLSX/CSV export. Any palette change runs the dataviz validator (FR-VIZ-07).

## Consulting uipro (the `ui-ux-pro-max` skill)

uipro is a **review lens**. Its recommendations never override the PRD or MASTER.md. Rev 0.2
rejected two of its outputs (OLED dark-only style; landing-page patterns), kept the
data-dense dashboard rules, and recorded IBM Plex as an alternative typeface.

```bash
S=.claude/skills/ui-ux-pro-max/scripts/search.py
python3 $S "focus not obscured" --domain ux            # one outcome per query
python3 $S "error summary validation" --domain ux
python3 $S "financial dashboard data-dense" --domain style
python3 $S "fintech accounting trust" --domain color
python3 $S "financial tabular figures" --domain typography
python3 $S "financial variance waterfall" --domain chart
python3 $S "icon button accessible label" --domain icons
python3 $S "theming dark mode tokens" --stack shadcn
python3 $S "large list virtualization bundle" --domain react
```

Rules: one intent per query (2–5 terms); verify the result's domain fits a web app behind a
login; retry once with a narrower query; record what you adopted (and why) in the PR or in
MASTER.md. Do not run `--design-system --persist --force` over the curated MASTER.md.

## Definition of done (UI changes)

1. Requirement IDs referenced; Basic/Expert tagging decided and registered.
2. Tokens only; `make check-tokens` passes; light, dark, high contrast and forced colours checked.
3. Accessibility checklist above ticked; axe clean.
4. Component tests, stories and visual snapshots updated (QA-UI-01…03); mode parity (QA-UI-04).
5. MASTER.md or a page override updated if the change sets a new pattern; prototype updated if
   it shows that screen.
6. Help keys, tooltip text and the guide page added (QA-UI-05); tour step updated if the screen
   is on the first-run path (QA-UI-06).
7. Commit with the `ui` or `design` scope (see **fyniq-release**).

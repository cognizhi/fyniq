# Design System Master · Fyniq

> **How to use.** When you build a page, first check `pages/<page>.md`. If it exists, its rules
> **override** this file for that page. Otherwise follow this file. The PRD (§7) states the
> requirements; this file states how they look and behave. The tokens are in `tokens.css` and
> are validated by `scripts/check_contrast.py`.

| | |
|---|---|
| Product | Fyniq: accounting, compliance, reporting and planning for Singapore start-ups |
| Name and line | **Fyniq** (one word, capital F), with the line *Intelligence behind every number* (13 Oct 2026). The line belongs on the sign-in page, the first-run welcome and outward material, never in place of a label. It is a promise the UI keeps: every figure opens to its source and history (FR-UX-04), and AI content says where its numbers come from (AI-68). Until a logo exists the wordmark is the name in the UI font; the organisation's own logo (FR-ORG-09) sits beside it |
| Users | Founders who are not accountants (Basic mode) and accountants, controllers and auditors (Expert mode) |
| Stack | React 19 SPA, shadcn/ui on Base UI, Tailwind CSS 4, AG Grid (entry grids), TanStack Table (statements), ECharts 6, Lucide icons |
| Origin | uipro (`ui-ux-pro-max`) review of PRD-001 on 12 Oct 2026, reconciled with the PRD; findings REV-29…40 in `docs/prd/reviews/prd-001-review-2026-10-12.md` |
| Dials | Variance 3 (minimal), Motion 3 (subtle), Density 8 (data-dense) |

## 1. Direction

**"Calm confidence with a spark."** Serious about numbers, quiet in chrome, with personality from
colour, motion and craft rather than decoration.

- **Style:** Minimal Swiss base (grid, clear type hierarchy, generous but disciplined white
  space), with uipro's **Data-Dense Dashboard** rules for working views: compact cards, sticky
  table headers, 8–12 px card padding, maximum data visibility without clutter.
- **Rejected from uipro:** the OLED dark-only style (light mode is the default here, and all
  four themes are required) and the landing-page patterns (the app sits behind a login).
- **One primary action per view.** Secondary actions are subordinate (outline or ghost); the
  rest go in "⋯".

## 2. Colour roles

All colours are semantic tokens (`tokens.css`). Components never use raw hex or Tailwind palette
classes (`bg-blue-500` is a review blocker).

| Role | Token(s) | Use | Never |
|---|---|---|---|
| Page and surfaces | `--background`, `--surface-1/2/3` | Page, cards and tables, subtle panels, hover and selection | Pure black in dark mode |
| Text | `--text-primary/secondary/muted` | Content, labels, help | Series or status colours for text other than chip labels |
| Brand (deep teal) | `--primary`, `--primary-hover`, `--primary-subtle`, `--link`, `--ring` | The one primary button, links, selection, focus rings | AI content; data series |
| **AI (violet)** | `--ai`, `--ai-subtle`, `--ai-subtle-foreground`, `--ai-border`, `--ai-gradient` | **Only** AI-originated content: assistant messages, proposals, suggestions, the "AI" badge | Buttons, links, brand moments, anything a human created |
| Status | `--status-{good,warning,serious,critical,neutral}` (+ `-subtle`) | Status chips, check results, banners, always with an icon and a label | Data series; decoration |
| Destructive | `--destructive` | Void, revoke, delete-like actions, visually separated from primary | Errors in form fields (use `--status-critical`) |
| Amounts | `--amount-positive/negative` | Optional colouring of amounts (user setting) | As the only cue: the sign or brackets always show |
| Charts | `--chart-1…8`, `--seq-*`, `--div-*`, `--viz-status-*` | Data marks only, per the dataviz rules (PRD §7.5) | Text, chrome, the brand accent |

**Gradient rule:** `--ai-gradient` appears only on the Home pulse card and the AI panel header.
Never put it on controls or behind body text.

**Themes:** `:root` (light), `.dark`, `[data-contrast="more"]` (7:1 text), forced-colours mode.
Set by the header theme menu and the command palette; "System" follows `prefers-color-scheme` and
`prefers-contrast`.

## 3. Typography

| Token | Size | Use |
|---|---|---|
| `--fs-xs` | 12 px | Captions, axis labels, badges |
| `--fs-sm` | 13 px | Compact tables |
| `--fs-base` | 14 px | UI body, comfortable tables |
| `--fs-md` | 16 px | Forms, reading text, all text on mobile (no iOS zoom) |
| `--fs-lg` | 18 px | Section titles |
| `--fs-xl` | 22 px | Page titles |
| `--fs-2xl` | 28 px | Stat tiles |
| `--fs-3xl` | 36 px | Home pulse figures |

- The `--fs-*` tokens are mapped onto Tailwind's `text-*` utilities (`@theme inline`), so
  `text-base` means 14 px in this app, deliberately smaller than Tailwind's 16 px default.
- **Inter** for UI with `font-variant-numeric: tabular-nums` on every number in tables, totals,
  tiles and axes. **JetBrains Mono** for codes, references and IDs. Both are self-hosted (SIL OFL),
  with `font-display: swap`. There are **no** Google Fonts requests.
- Weights: 400 body, 500 labels and table headers, 600 titles and totals. No 300 weight below
  16 px.
- Line height 1.5 for reading text and 1.25 for dense tables. Reading text measure ≤ 75
  characters.
- Alternative (recorded, not chosen): IBM Plex Sans and Mono, uipro's "Financial Trust" pairing.

## 4. Numbers

- Right-aligned, tabular, thousands separators, currency code in the column header (not on every
  cell).
- Negatives: "−1,234.56" (U+2212) or "(1,234.56)" per user preference. **The clipboard and
  exports always use an ASCII "-" and plain decimals** (FR-UX-20).
- Totals rows: weight 600 with a top border of `--border-strong`. Never re-round: totals are
  sums of stored amounts.
- Zero shows as "0.00" in statements and as "–" in sparse grids (consistent per table).
- Ledger-derived figures render only from server responses. There are **no optimistic money
  updates** (FR-UX-22).

## 5. Layout and density

| Token | Value |
|---|---|
| Grid | 4 px; spacing tokens `--space-1…8` (4–32 px) |
| Header | 56 px, sticky; holds the org switcher, ⌘K search, **Basic / Expert toggle**, theme, notifications, user |
| Sidebar | 240 px; collapses to 64 px icons with labels in tooltips |
| Side-peek / AI panel | 420 px from the right; never covers the focused element (NFR-A11Y-03) |
| Rows | 40 px comfortable, 32 px compact (`[data-density="compact"]`) |
| Breakpoints | 375 / 768 / 1024 / 1440. Below 1024 px the sidebar becomes a drawer; below 768 px, grids become cards and statements scroll horizontally inside their own region with a sticky first column |
| Targets | ≥ 24 × 24 px pointer targets everywhere (WCAG 2.2 SC 2.5.8); ≥ 44 px on touch in the PWA |
| Z-index | sticky 10 · header 20 · panel 30 · popover 40 · modal 50 · tour 55 · toast 60 (`--z-*` tokens) |

## 6. Experience modes: Basic and Expert (PRD §7.8)

- A **segmented control** labelled "Basic | Expert" sits in the header, always visible
  (`role="radiogroup"`, arrow keys switch). Also available from ⌘K: "Switch to Expert mode".
- Switching is instant, keeps the page, filters and unsaved drafts, and never changes data,
  permissions or API calls.
- **Basic** hides expert navigation, field groups, grid tooling and the single-key shortcuts.
  It never hides ✖/⚠ statuses, approvals, locked periods, deadlines, AI labels or books health.
- A record that uses hidden fields shows a disclosure row ("Has advanced details (2) ▸") that
  opens them read-only, with "Open in Expert mode".
- Expert features stay findable: ⌘K results in Basic mode show them with an "Expert" tag, and
  choosing one offers to switch.
- Every route, nav item, field group and action is tagged `basic` or `expert` in one registry
  (`apps/web/src/ui/modes.ts`); see FR-UX-29 and QA-UI-04.

## 7. Components (shadcn/ui on Base UI)

| Component | Rules |
|---|---|
| Button | Variants: primary (one per view), secondary (outline), ghost, destructive (separated from primary by space or a divider). Loading state disables the button and shows a spinner. Never style a link as a primary button. |
| Status chip | Lucide icon + text label + subtle background; tooltip says what is wrong and how to fix it; "Fix it" action where possible. Mapping: ✔ `circle-check` good · ⚠ `triangle-alert` warning · ✖ `circle-x` critical · ⏰ `clock-alert` serious · ✎ `pencil-line` neutral · 🔒 `lock` neutral. The symbols themselves appear only in exports and plain text. |
| AI badge | `sparkles` icon + "AI" label on `--ai-subtle` with `--ai-subtle-foreground`; on every AI message, proposal, suggestion and AI-drafted text until a human edits and saves it. |
| Proposal card | `--ai-border` left rule (3 px) plus the AI badge; plain summary, item list, ledger impact table, diff, evidence links, risk tier; **Approve is not focused by default**; tier 2–3 shows "what could go wrong". |
| Ledger impact table | Debit / Credit / Account (Expert) or a one-line plain summary (Basic); totals always balance visibly. |
| Data grid (AG Grid) | Sticky header and first column; range select; Ctrl+D fill-down (keyboard alternative to dragging); paste preview; inline status chips; virtualised; row height from the density token. |
| Statement table (TanStack) | Semantic `<table>` with `<th scope>`; `aria-sort` on sortable columns; printable; each figure is a drill-down button (FR-UX-04). |
| Stat tile | Label, figure (`--fs-2xl`, tabular), change vs comparison with a sign and an arrow icon (never colour alone), "updated" time, drill-down. |
| Command palette (cmdk) | ⌘K / Ctrl+K; recent items first; groups: Go to, Create, Reports, Actions, Ask AI (Tab); Expert-only results tagged in Basic mode. |
| Toast | Polite live region; never steals focus; 4–6 s; actions such as "Undo" stay until dismissed. Not used for errors that need a fix (those go inline). |
| Dialog / sheet | Opens from its trigger; Esc closes; confirms before discarding unsaved changes; typed confirmation for bulk voids. |
| Empty state | Explains what goes here, with the first action and a line illustration (none in reports). |
| Form field | Visible label, helper text, error below the field, linked with `aria-describedby`; validates on blur; an error summary takes focus after a failed submit. |
| Info button and popover | Beside a visible label; see §12. |
| Walkthrough (tour) | Scrim, spotlight and caption card; see §12. |

## 8. Icons

**Lucide** only: 16 / 20 / 24 px, 1.5 px stroke, the same style at each hierarchy level.
Decorative icons beside text get `aria-hidden="true"`; icon-only buttons get an accessible name
and a tooltip. uipro's default is Phosphor, but shadcn/ui's own components use Lucide, and one
set is a hard rule.

## 9. Motion

- 120 ms (fast) / 180 ms (base) / 240 ms (slow); exits ≈ 70% of entrances; `transform` and
  `opacity` only. The walkthrough spotlight is the one exception: it moves with `transform` and
  resizes with `width`/`height` over 300 ms (`--duration-tour`), one element at a time (§12).
- Side panels slide from the right; dialogs scale and fade from their trigger; list items do not
  animate on data refresh.
- Number tickers on the Home pulse card only, never in reports or grids.
- `prefers-reduced-motion`, or the user's in-app Reduce motion setting (`[data-motion="reduce"]`):
  every duration is 0 and final states render immediately.
- Exactly one "moment": the month-end "Books closed" confirmation (no confetti).

## 10. Charts

Follow PRD §7.5 and the dataviz method: pick the form first, one y-axis, categorical colours in
fixed order (`--chart-1…8`, more than 8 series fold into "Other"), sequential = one hue,
diverging = blue ↔ red with a grey midpoint, status colours reserved, a legend for ≥ 2 series with
direct labels for ≤ 4, hover tooltips that are also keyboard-reachable, a table view toggle, and
PNG and XLSX/CSV export. Waterfalls for bridges (cash bridge, budget variance): ≤ 12 bars, with
signed values and arrow icons, not red and green alone. The palette is validated in CI
(FR-VIZ-07).

## 11. Accessibility (WCAG 2.2 AA, with AAA where stated)

- Text contrast 4.5:1 (7:1 in high contrast); control boundaries and focus rings 3:1. Enforced
  by `scripts/check_contrast.py`.
- Visible focus ring: 2 px `--ring` plus a 2 px offset; never removed; never hidden by sticky
  regions, panels or toasts (SC 2.4.11).
- Full keyboard operation; skip link; logical tab order; focus moves to the main region on route
  change.
- Single-key shortcuts can be turned off or remapped (SC 2.1.4); they never fire in text fields.
- Every drag has a click or keyboard alternative (SC 2.5.7).
- Authentication allows password managers, paste and passkeys (SC 3.3.8).
- Live counts are announced as contextual phrases in one polite `role="status"` region.
- Charts: a text summary, a table view, and keyboard-reachable tooltips.

## 12. Help and onboarding (PRD §7.9)

Help lives on each page, where the question comes up. Four layers, each with one job:

| Layer | Use for | Anatomy | Rules |
|---|---|---|---|
| **Tooltip** | The name of an icon-only control, a truncated value, a chart mark | ≤ 80 characters; no links or formatting | Opens on hover **and** keyboard focus; `role="tooltip"` linked with `aria-describedby`; never the only place a label exists |
| **Info popover** | What a figure, field, column, status or term means, why it matters, an example (FR-UX-30) | Lucide `info` (16 px) in a 24 × 24 px button **beside the visible label**; title, two or three plain sentences, the accounting detail in Expert mode (account, posting, rule ID, `--text-muted` above a divider), "Learn more" to the guide page | Opens on hover or focus (transient) and on click or Enter (pinned); stays open while the pointer is over it; Esc closes and returns focus to the button; an outside click closes it; inserted right after its button, so Tab reaches "Learn more"; `aria-expanded` and `aria-controls`; a non-modal `role="dialog"` labelled by its title; `--z-popover` |
| **User guide** | Tasks and concepts across the product (FR-UX-32) | `/help`; see `pages/help.md` | Every popover and empty state links to a guide page |
| **Walkthrough** | A user's first sign-in; replayable from Help (FR-UX-34) | Scrim, spotlight, caption card | Below |

**Content.** Plain English, the UI's own names, sentence case, one idea per sentence. Basic mode
explains without debits and credits; Expert mode adds the account, the posting and the rule ID.
Text comes from the help registry key of the feature (FR-UX-31), never hard-coded, and the
accountant reviews any accounting or statutory statement.

**WCAG 2.2.** Content on hover or focus is dismissible (Esc), hoverable and persistent (SC 1.4.13);
info buttons are at least 24 × 24 px (SC 2.5.8); popovers and the caption card never cover the
focused element (SC 2.4.11); every part has a name, role and state (SC 4.1.2).

### Walkthrough anatomy

| Part | Spec |
|---|---|
| Scrim | `--overlay-scrim` around the target, drawn as a 9999 px spread shadow on the spotlight. The tour is **non-modal**: pointer events pass through and the page stays usable |
| Spotlight | The target's box plus 6 px, `--radius-l`, a 2 px `--primary` ring (brand teal, never the AI violet). Moves between steps with `transform` and resizes with `width`/`height` over `--duration-tour` (300 ms, `--ease-out`). In forced-colours mode a 3 px `Highlight` outline replaces the ring and scrim |
| Caption card | ≤ 340 px, `--surface-1`, `--shadow-3`, `--radius-l`, 16 px padding: "2 of 7", a title (`--fs-md`, 600), one or two sentences, progress dots (decorative, `aria-hidden`), then **Skip tour** (ghost) · **Back** · **Next** (primary; **Done** on the last step). Placed below the target, else above, else beside it; always inside the viewport with an 8 px margin |
| Layer | `--z-tour` (55): above dialogs, below toasts |
| Steps | At most 7, about 2 minutes, a variant per role. A step whose target is hidden (mode, breakpoint, feature switch) is skipped |
| Keyboard and screen readers | Non-modal `role="dialog"` labelled by the title and described by the text. On start, focus moves to the title; Next and Back work as buttons or with ← / → inside the card; Esc closes. Each step is announced in a polite live region ("Step 3 of 7. Your Inbox. …"). On close, focus returns to where the user was, or to the page heading |
| Reduced motion | `--duration-tour` is 0 under `prefers-reduced-motion` or `[data-motion="reduce"]`: the same steps, no movement |
| State | Finished or skipped is stored per user on the server; the tour does not start again by itself. "Take the tour again" in Help and ⌘K replays it |
| Never | Click, type or change data for the user; advance on a timer; block the page; cover the focused element |

The prototype (`docs/design/prototype/index.html`) shows all four layers: `?tour=1` replays the
tour, `?motion=reduce` removes the motion, `?pop=runway` opens an info popover, and
`#help/founders-money` opens a guide page.

## 13. Anti-patterns (review blockers)

- Raw colours or Tailwind palette classes in components; one-off shadows or radii.
- The AI violet used for anything not AI-originated, or the brand teal on AI content.
- Emoji or Unicode symbols as structural icons in the UI.
- Colour as the only carrier of meaning (status, sign, series).
- Optimistic updates of ledger figures; numbers re-rounded in the UI.
- Third-party font, script or analytics requests.
- Dual-axis charts; rainbow scales; series colours for text.
- Modals for primary navigation; hover-only affordances; toasts for errors that need a fix.
- Basic mode silently hiding a value that is set on a record.
- A tooltip or popover as the only label of a control; help text hard-coded instead of a help key;
  a tour that clicks, types or auto-advances for the user.

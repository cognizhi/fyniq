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
| Origin | uipro (`ui-ux-pro-max`) review of PRD-001 on 12 Oct 2026, reconciled with the PRD; findings REV-29…40 in `docs/prd/reviews/prd-001-review-2026-10-12.md`. Device classes and motion (§5.1, §9) added for PRD rev 0.10 after a second uipro pass (13 Oct 2026, below) |
| Screens | Every screen is designed in Claude Design canvases built from `docs/design/screens/` at the five reference frames of §5.1, in light and dark. The canvases are the visual reference; the static prototype is superseded |
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
| Breakpoints | Device classes, not devices (§5.1): **compact** < 600, **medium** 600–1023, **expanded** ≥ 1024 (sidebar collapsed to 64 px below 1280). Layout is mobile-first: base styles are compact, and `min-width` queries add the larger layouts |
| Targets | ≥ 24 × 24 px pointer targets everywhere (WCAG 2.2 SC 2.5.8); ≥ 44 × 44 px on touch screens (`@media (pointer: coarse)`, `--touch-target`) |
| Z-index | sticky 10 · header 20 · panel 30 · popover 40 · modal 50 · tour 55 · toast 60 (`--z-*` tokens) |

### 5.1 Device classes (PRD §7.10, FR-UX-37; rev 0.10)

| Class | Width | Reference frames | Navigation | Lists and records | Panels |
|---|---|---|---|---|---|
| **Compact** | < 600 px | Phone 390 × 844; foldable folded 344 × 882 | Top bar 56 px: menu, page title, search, AI. Bottom bar 64 px (`--bottom-bar-height`): Home, Inbox, the role's main area, Banking, More (≤ 5, icon + label). The full navigation opens from More as a sheet | One column. Grids become cards (title, amount, status chip, one secondary line). Record tabs become a horizontally scrolling tab strip; the right panel moves below the body. Statements scroll inside their region with a sticky first column | Side-peek, the AI panel and filters open full screen with a close button and a back gesture |
| **Medium** | 600–1023 px | Foldable unfolded 690 × 840; tablet portrait 834 × 1194 | Navigation rail 80 px (`--rail-width`): icons with short labels, badges; the header keeps search, the mode toggle and the AI button | List and detail side by side where the screen has both (list 320 px, detail fills); full grids with the key columns; the column chooser adds the rest | Side-peek and AI panel as a 420 px sheet over the content with a scrim |
| **Expanded** | ≥ 1024 px | Desktop 1440 × 900 (tablet landscape and laptops fall here) | Sidebar 240 px; collapsed to 64 px icons below 1280 px or on request | As §7: full grids, record anatomy with the right panel | AI panel and side-peek dock beside the content (420 px) |

- **Same content in every class.** A class changes layout, never information: statuses, approvals,
  locked periods, deadlines, AI labels and books health appear everywhere (FR-UX-24, FR-UX-37).
- **Foldables.** The cover screen (344 px) is the narrowest target: nothing may need more. When a
  foldable opens or closes, the page, scroll position, filters and drafts stay. On a device that
  spans a hinge (`@media (horizontal-viewport-segments: 2)`), a two-pane layout puts the list in the
  first segment and the detail in the second, using `env(viewport-segment-right 0 0)` and
  `env(viewport-segment-left 1 0)`, and nothing straddles the hinge. Table-top posture
  (`vertical-viewport-segments: 2`) keeps reading content in the top half and controls in the bottom.
- **Primary action.** In compact, the one primary action of a page sits in a sticky bottom action
  bar above the bottom navigation (or replaces it on editors), never in a floating button that
  covers content.
- **Safe areas.** Fixed bars pad with `env(safe-area-inset-*)`; scroll content is padded so nothing
  hides behind a fixed bar (SC 2.4.11).
- **uipro (13 Oct 2026).** Adopted: mobile-first `min-width` queries; 24 px web and 44 px touch
  targets; one primary action per screen; bottom navigation at most five labelled items;
  `prefers-reduced-motion`; easing by purpose. uipro has no guidance for foldables, hinges or
  list-detail layouts (no database match after a narrower retry), so those rules follow the W3C
  Viewport Segments and Device Posture drafts and the common window-size-class pattern. Rejected
  again: its dark-first navy and green palette, the Fira pairing and the landing-page pattern
  (PRD §7.4 fixes the brand and type).

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
  (`apps/web/src/registry.ts`, with the help text in `src/i18n/`); see FR-UX-29 and QA-UI-04.

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
| Review banner ("Needs additional verification", FR-UX-41, rev 0.22) | Warning banner (`triangle-alert` + heading) directly under the page header, on the pages the review register (`docs/prd/reviews/review-status.json`) lists for a pending area; one line per area: bold "<rules> await review by <reviewer>." then what to check. "What this means" opens the help page (`review.awaiting-verification`). A collapse button (`chevron-up`, `aria-expanded`) shrinks it to one line; it cannot be dismissed or switched off, and it shows in Basic and Expert. Never on sign-in, consent, recovery or self-service pages; never AI violet. `build.py` inserts it from the register, so fragments never hard-code it. Nemura follows the same rule (NM-UX-06). |

## 8. Icons

**Lucide** only: 16 / 20 / 24 px, 1.5 px stroke, the same style at each hierarchy level.
Decorative icons beside text get `aria-hidden="true"`; icon-only buttons get an accessible name
and a tooltip. uipro's default is Phosphor, but shadcn/ui's own components use Lucide, and one
set is a hard rule.

## 9. Motion (PRD §7.4, FR-UX-38)

Minimal and purposeful: motion shows where something came from or what changed, and never delays
input. Durations 120 ms (fast) / 180 ms (base) / 240 ms (slow); exits ≈ 70% of entrances;
`transform` and `opacity` only; ease-out (`--ease-out`) on entry, ease-in on exit, linear only for
progress. The walkthrough spotlight is the one exception: it moves with `transform` and resizes
with `width`/`height` over 300 ms (`--duration-tour`), one element at a time (§12).

**The catalogue** (nothing else animates):

| Motion | Where | Spec |
|---|---|---|
| Page enter | Main region on route change | Fade in with an 8 px rise (`--rise`), `--duration-base` |
| Panel and sheet | Side-peek, AI panel, sheets, the More sheet | Slide from its edge (right; bottom in compact), `--duration-slow`; exit `--duration-base` |
| Dialog | Dialogs and the step-up prompt | Scale 0.98 → 1 and fade from the trigger, `--duration-base` |
| List entry | First render of a list, card stack or Inbox group | Rows fade and rise in a stagger of `--stagger` (30 ms), at most 8 rows; never on a data refresh |
| Skeleton | Content loading over 300 ms | A soft sheen across `--surface-2` → `--surface-3`, 1.2 s linear; `aria-busy` on the region |
| Status change | Chip or count changing value | Cross-fade, `--duration-fast`; the polite live region announces it (FR-UX-21) |
| Pulse ticker | Home pulse card figures only | Counts up to the server value once, ≤ 600 ms; never in reports or grids |
| Books closed | Month-end close confirmation | The check draws (stroke offset), then the card settles; once, no confetti |
| Hover and press | Buttons, rows, cards | Colour and shadow transitions, `--duration-fast`; no movement on hover |

**Switches.** The Administrator sets organisation motion **On** (default) or **Off** in Settings ›
Appearance and motion (`[data-motion="off"]` on the root). A user can choose Reduce motion
(`[data-motion="reduce"]`), and `prefers-reduced-motion` always wins. Off and Reduce set every
duration to 0 and render final states immediately; skeletons keep a static fill.

## 10. Charts

Follow PRD §7.5 and the dataviz method: pick the form first, one y-axis, categorical colours in
fixed order (`--chart-1…8`, more than 8 series fold into "Other"), sequential = one hue,
diverging = blue ↔ red with a grey midpoint, status colours reserved, a legend for ≥ 2 series with
direct labels for ≤ 4, hover tooltips that are also keyboard-reachable, a table view toggle, and
PNG and XLSX/CSV export. Waterfalls for bridges (cash bridge, budget variance): ≤ 12 bars, with
signed values and arrow icons, not red and green alone. The palette is validated in CI
(FR-VIZ-07). `--chart-7` is a violet, but it is a data colour, not the AI violet: charts never
use the `--ai` tokens, the violet series appears only from the seventh series on, and AI content
is always marked by the "AI" badge, never by hue alone (PRD §7.5, rev 0.11).

Every chart in the web app is the `Chart` component (`apps/web/src/components/chart`), which applies these rules: the
caller picks the form (`line`, `bar`, `bar-horizontal`) and gives each entity a fixed `slot`; the component folds a ninth
series into "Other", shows the legend and direct labels, moves the tooltip with the arrow keys, offers the table view
and the PNG and CSV downloads, and loads ECharts only when a chart is first shown. Its option is a pure function
(`chartOption`), so a test can check that the table's figures are the plotted ones.

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

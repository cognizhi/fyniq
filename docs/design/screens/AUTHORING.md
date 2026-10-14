# Designing a Fyniq screen

Every screen in `inventory.json` is a **fragment** in `src/<File>.html`. `build.py` wraps it in the
shared app shell (sidebar, rail, top bar, bottom bar), expands icons, lints it, and lays it out on
a Claude Design canvas at five frames in light and dark (PRD-001 §7.10, MASTER.md §5.1):

| Frame | Size (CSS px) | Device class |
|---|---|---|
| Desktop | 1440 × 900 | expanded (≥ 1024) |
| Tablet | 834 × 1194 | medium (600–1023) |
| Foldable open | 690 × 840 | medium |
| Phone | 390 × 844 | compact (< 600) |
| Foldable folded | 344 × 882 | compact |

One fragment serves all five: it is **responsive by CSS**, never five copies. Read
`src/Home.html` first; it is the reference screen. Then `kit/ui.css` for the classes.

## 1. The fragment

Two products share this kit (PRD-002 ADR-N6). A canvas's `product` in `inventory.json` picks the tokens, kit,
fonts and shell: **Fyniq** (default; `kit/fyniq-ui.css`) or **Nemura** (`kit/nemura-ui.css` = Nemura's tokens +
`kit/ui.css` + `kit/nemura.css`). Nemura fragments use the same `fy-` structure plus `nm-` components
(`docs/design-system/nemura/MASTER.md` §6), and set `persona` (`employee` | `hr` | `admin`) and `integrated`
(shows the Finance | People switch). Fyniq's finance shell shows the same switch by default (links, `.fy-workspace`),
because Cognizhi is linked; a fragment for someone without Nemura rights (an auditor, the Super admin) sets
`workspace: false`.
Nemura pages never show company financial figures (PRD-002 NM-ROLE-02). A Fyniq fragment for someone whose
rights are personal only sets `persona` (`employee` | `linemanager`) to draw the **personal workspace**
(PRD-001 FR-UX-39): only their own items and Help, no finance area, Home, Inbox or mode switch; with
`integrated: true` its header has Back to People instead of the switch (example: `PersonalClaims`).
`persona: "budgetholder"` draws a finance workspace cut to that role's areas (FR-UX-40, `DepartmentHome`).
Nemura also has `owner` (Arjun Rao) and `leaver` (Nurul Aziz, own documents only).

```html
<!-- fyniq-screen {"nav": "banking", "title": "Categorise", "mode": "basic",
                   "heights": {"compact": 1800, "medium": 1300, "expanded": 1000}} -->
<div class="fy-page fy-enter">
  …the page content…
</div>
<!-- @ai -->
<aside class="fy-ai" aria-label="AI assistant">…</aside>   (only when the AI panel is open)
```

| Setting | Meaning |
|---|---|
| `nav` | The active navigation key: `my-claims`, `money-in`, `home`, `inbox`, `sales`, `purchases`, `contacts`, `banking`, `payroll`, `projects`, `accounting`, `reports`, `planning`, `equity`, `compliance`, `settings`, `help` |
| `title` | The short title in the compact top bar |
| `mode` | `basic` or `expert`: the mode the canvas shows by default (the Tweaks panel switches it) |
| `layout` | `bare` for screens without the app shell (sign-in, first-run set-up, portals, emails). The fragment then provides everything inside the device, starting with `<div class="fy-bare">` or `<div class="fy-bare fy-bare--split">` |
| `heights` | The content height per class, so the frame shows the whole page (long-screenshot style). Too tall shows empty ground, too short clips; frames never go below the device size. Measure them: `build.py heights <File> --write` (a dialog over the page is not measured: set those by eye with `build.py shots`) |

**Review banner (FR-UX-41).** Never write the "Needs additional verification" banner into a fragment. `build.py`
inserts it under the page header of every screen the review register (`docs/prd/reviews/review-status.json`) lists for
a pending area, and adds room for it to the frame heights; `build.py heights` measures without it. When an area is
marked reviewed, rebuild and republish its canvases and the banner is gone.

## 2. Rules the canvas enforces (each one fails silently)

- Close every element (`<span></span>`, `<path …></path>`); quote every attribute; void elements
  (`<input>`, `<img>`, `<br>`, `<hr>`) need no closing tag.
- No `<script>`, `<style>`, `<link>`, `<form>`, `<iframe>`, no `on…` handlers, no `{{` or `}}` in text.
- No `checked`/`selected`/bare boolean attributes. Selection and toggles are `<button>`s with
  `aria-checked`, `aria-selected`, `aria-pressed` or `aria-current`.
- Inputs: `<input class="fy-input" id="…" value="…" readonly="readonly">` for filled values, or
  `placeholder="…"` for empty ones. Every input has a `<label for="…">`.
- No raw colours anywhere (`#…`, `rgb()`, `oklch()`): use `var(--token)` in an inline style when the
  kit has no class. Prefer kit classes; inline styles only for one-off layout (widths, gaps).
- No emoji and no Unicode symbol icons (✔ ✖ ⚠ ✎ ⏰ 🔒). Use Lucide icons with a text label.
- Icons: `<i data-icon="landmark"></i>` (any Lucide name; see `kit/lucide-icon-nodes.json`), sizes
  `class="fy-i--20"` / `fy-i--24` / `fy-i--32` / `fy-i--48`. Decorative by default; an icon that is
  the only content of a button needs `aria-label` **on the button**.
- Real elements: `<a href>` for navigation, `<button type="button">` for actions, `<table>` with
  `<th scope="col">` for tabular data, headings in order (one `h1` per page).
- `python3 docs/design/screens/build.py check <File>` must report 0 errors. Unknown `fy-` classes
  are warnings: fix the typo or use an inline style.

## 3. Responsive by class

Write mobile-first: the base is compact, the kit adds medium and expanded layouts. Use these and
the page adapts by itself:

| Need | Use |
|---|---|
| Columns that collapse | `fy-grid fy-grid--2/3/4` (1 → 2 → N), `fy-grid--tiles` for stat tiles |
| Content + side column | `fy-split` (stacked until expanded, then main + 340 px aside) |
| Side navigation + content (Help, settings) | `fy-sidenav` (stacked until expanded, then a sticky 260 px left column) |
| List and detail (Inbox, invoices, bills, contacts) | `fy-listdetail` with `fy-listdetail__list` and `fy-listdetail__detail`: one pane in compact (set `data-show="detail"` to show the detail), two panes from medium |
| Tables that become cards | `<table class="fy-table fy-table--cards">` and on each `td` a `data-card`: `title`, `amount`, `meta`, `status` or `hide` |
| Wide statements | `<div class="fy-tablewrap"><table class="fy-table fy-statement">…` (scrolls in its box with a sticky first column) |
| Show per class | `fy-only-compact`, `fy-from-medium`, `fy-only-medium`, `fy-from-expanded`, `fy-until-expanded`, `fy-hide-medium`; on `th` and `td` these hide table columns per class |
| Text that must not truncate (safety, statuses) | `fy-wrap` on the element (overrides `fy-truncate` and list-row truncation) |
| The primary action on phones | `fy-actionbar` (sticky at the bottom in compact, inline from medium). Header buttons can be `fy-from-medium` with the same action in an `fy-actionbar fy-only-compact` |
| Modes | `fy-expert-only` / `fy-basic-only` on anything that differs; safety information is never mode-only |

Compact rules: one column; 16 px text in forms; ≥ 44 px targets; tabs scroll horizontally; side
panels become full-screen sheets; no hover-only affordances; nothing wider than 344 px may be
required (the folded foldable).

## 4. The components (kit/ui.css)

Page: `fy-page` (+ `--narrow` for forms), `fy-pagehead` (`__text`, `__actions`), `fy-breadcrumb`,
`fy-h1/h2/h3`, `fy-eyebrow`, `fy-lead`, `fy-prose`, `fy-section`, `fy-card` (`--pad`, `--flat`,
`--sunken`, `__head`, `__body`, `__foot`), `fy-dl` (`--3`), `fy-divider`, `fy-row`, `fy-stack`
(`--2/3/6`).
Controls: `fy-btn` (`--primary` once per view, `--secondary`, `--ghost`, `--destructive`, `--sm`,
`--lg`, `--block`), `fy-iconbtn`, `fy-segmented`, `fy-tabs`, `fy-filter`, `fy-chipset`, `fy-kbd`,
`fy-switch` (button `role="switch" aria-checked`), `fy-check`, `fy-choice` + `fy-radio`, `fy-info`.
Forms: `fy-form` (`--2`), `fy-field` (`--full`), `fy-fieldset`, `fy-label` (+ `fy-req`),
`fy-input`, `fy-select`, `fy-textarea`, `fy-inputgroup` + `__addon`, `fy-help`, `fy-error`,
`fy-suggest` (an AI-suggested value; violet text, says so).
Status: `fy-chip` (`--good`, `--warning`, `--serious`, `--critical`, `--neutral`, `--info`,
`--outline`), `fy-badge` (`--quiet`, `--dot`), `fy-tag` (`--expert`), `fy-banner` (`--info`,
`--warning`, `--critical`, `--good`, `--strip`), `fy-progress` (`--good/warning/critical`),
`fy-steps` (`--wrap`, `--vertical`) + `fy-step` (`--done`, `aria-current="step"`), `fy-banner__action` (the banner's buttons; wraps under the text on phones).
Data: `fy-tile` (`__label`, `__value`, `__meta`), `fy-delta` (`--up/down/flat`), `fy-pulse`
(Home only), `fy-table` (`--compact`, `--cards`, `fy-statement`, `fy-group` rows, `fy-sub` cells,
`tfoot` totals), `fy-tablewrap`, `fy-list` + `__item/__main/__title/__meta/__icon` (`--ai`,
`--critical`, `--warning`, `--good`, `--brand`), `fy-ledger` + `fy-acct`, `fy-plain`,
`fy-timeline` (`__item`, `__dot` `--good/--ai`), `fy-board` (`__col`, `__card`; `--fit` fits every column on desktop instead of scrolling), `fy-cal`
(`__dow`, `__day`, `--muted`, `--today`, `__n`, `__ev` `--due/--done`), `fy-empty` (`__art`),
`fy-skel` (`--text/title/block`), `fy-avatar` (`--sm/lg`), `fy-avatars`, `fy-num`, `fy-mono`.
Charts: inline `<svg class="fy-chart" viewBox="…" role="img" aria-label="one-sentence summary">`
with `.grid` / `.axis` lines, marks `fy-bar fy-c1…c8` (`<rect>`), `fy-line fy-c1` (`<polyline>`),
`fy-area fy-c1`; sequential `fy-seq1…5`; diverging `fy-divneg/fy-divpos/fy-divmid`; legends
`fy-legend` + `fy-legend__key fy-swatch-1…8`. One y-axis per chart; a "Table" toggle beside it.
AI (violet, AI-made content only): `fy-ai-badge` (always with `<i data-icon="sparkles"></i>AI`),
`fy-ai-card` (`__head`, `__body`), `fy-ai-text`, `fy-source` (where the figures come from),
`fy-fact` (a figure from a read tool), the panel `fy-ai` (`__head`, `__context`, `__thread`,
`__composer`) with `fy-msg--user` / `fy-msg--ai`.
Overlays (inside the fragment, they cover the app): `fy-scrim` (+ `--right`) holding `fy-dialog`
(`--wide`, `__head/__body/__foot`, `fy-grabber` for the phone sheet) or `fy-sheet`; `fy-palette`
(`__input`, `__group`, `__item`, `__foot`); `fy-popover`, `fy-tooltip` (absolutely positioned;
set `top`/`left` inline); `fy-toast`; `fy-tour-spot` + `fy-tour-card` + `fy-dots`.
Bare screens: `fy-bare` (+ `--split`, `__card`, `__aside`, `__main`).
Other: `fy-dropzone`, `fy-receipt` (a receipt image placeholder).

## 5. Motion (FR-UX-38)

Put `fy-enter` on the page root and `fy-stagger` on the first list or card stack the eye goes to
(at most one or two per screen). Panels, dialogs, toasts, skeletons, progress bars and chart bars
animate by themselves. Nothing else. Motion runs only when the device's `data-motion` is `on`.
For the "Books closed" moment, give the check icon's SVG the class `fy-draw`.

## 6. Content: one company, consistent everywhere

Today is **Tuesday 13 Oct 2026, 09:12**. Every screen shows the books at that moment: nothing
happened later (only scheduled things lie ahead), a bank line that is still to categorise leaves
its invoice open, and an auto-matched one has paid it. Currency SGD (S$), dates "13 Oct 2026",
amounts with two decimals in tables.

**`sample-data.json` is the register.** It holds the chart of accounts (one name per code, from
PRD Appendix B.5 where B.5 assigns one), every contact, every person's role and masked ID, every
document number the screens use (invoice, bill, journal, claim, quote, purchase order, credit
note), every AI proposal number, and the headline figures (cash, receivables, payables, the
September P&L, equity, payroll, payment batches, checks, officers and deadlines, AI usage and
connections). Look a fact up there before you write it; add a new document or proposal there
first. `build.py check` fails when a screen gives an account code another name, puts a document
number next to the wrong contact, uses a document or proposal number the register doesn't know,
names the wrong weekday for a date, shows a time after 09:12 that isn't scheduled, or masks
someone's ID differently. Figures are not linted: copy them from `figures` exactly, and when a
total changes, change every screen that shows it (search the fragments for the old value).

| Fact | Value |
|---|---|
| Company | **Cognizhi Pte Ltd**, UEN 202412345K, incorporated 2 Sep 2024, GST-registered from 1 Jan 2026 (GST no. 202412345K), quarterly returns, financial year Jan–Dec, base currency SGD |
| People | **Mei Lin Tan** (CEO, founder, Owner, director; Basic mode; avatar ML) · **Arjun Rao** (CTO, co-founder, director; avatar AR) · **Grace Ho** (external accountant, Ho & Partners LLP; Expert) · employees Daniel Lim (senior software engineer), Wei Jie Ong (engineering lead), Chen Yu (data scientist, Employment Pass, leaving 30 Nov), Priya Nair (head of customer success), Hafiz Rahman (customer support), Sarah Koh (office and people manager): 6 on payroll · **Rachel Teo** (company secretary, Ho & Partners LLP) · Nurul Aziz (left 31 Jul 2026; exercised options 8 Oct) |
| Team mode | Founder mode (FM-1…9 apply: two-person rule, self-approval up to S$2,000) |
| Bank accounts | DBS Business Multi-Currency (SGD) S$287,940.22 · OCBC Business Growth (SGD) S$96,115.80 · Wise (USD) US$15,862.65 ≈ S$20,410.47 · Airwallex (USD) US$6,150.35 ≈ S$7,913.66 · total cash **S$412,380.15** |
| Runway | 10.8 months at S$38,214 average monthly burn (Jul–Sep) |
| Customers | Acme Analytics Pte Ltd · Lumen Health Pte Ltd · Orbital Freight Asia Pte Ltd · Kopi Labs Pte Ltd · Harbourfront Dental Group |
| Suppliers | Amazon Web Services · Google Workspace · WeWork Singapore · Figma · Grab for Business · SP Group · Ho & Partners LLP |
| Numbers | Invoices INV-0096…INV-0151, bills BILL-0790…BILL-0929, journals JNL-0001…JNL-0249, claims CLM-0038…CLM-0051, quotes QUO-0024…QUO-0032, purchase orders PO-0013…PO-0016, credit note CN-0012: each one is in `sample-data.json`. INV-0142 to Lumen Health can't post (no GST code on line 2); BILL-0918 (AWS, S$2,861.40) waits for Mei Lin's approval; pay run PR-2026-10 is paid on Wed 28 Oct |
| Periods | Aug 2026 closed and locked; Sep 2026 close in progress (4 of 7 steps; Grace Ho reviewing); GST return Jul–Sep due 31 Oct; CPF for Oct due 14 Nov |
| Counts shown in the shell | Inbox 5 · Sales 3 overdue · Purchases 2 due · Banking 14 to categorise · Compliance 1 due · My claims 1 returned |
| Projects | Lumen Health pilot (fixed fee S$48,000) · Acme data platform (time and materials) |
| Checks | 90 (36 from the workbook, 54 platform): 1 can't post, 3 to review, 1 not assessed, 85 OK |
| AI | Claude and Gemini on Agent Platform, Singapore, both on; OpenAI API waiting for Arjun Rao (know-how only); OpenRouter a draft; the company secretary agent on since 1 Oct; S$120.86 spent 1–13 Oct (`figures`). Connected apps run on their own model: no Fyniq tokens or cost |
| Equity | Mei Lin Tan 4,500,000 and Arjun Rao 4,500,000 ordinary shares; option pool 1,000,000 (ESOP 2026); a post-money SAFE from Lion Rock Ventures, S$500,000 at an S$8,000,000 cap, signed 18 Jun 2026 and held as a liability (2700); share capital S$90,000.00 (S$0.01 a share) |

Copy is plain English, sentence case, the UI's own names. Basic mode avoids debit, credit and
account codes; Expert adds them (`fy-expert-only`). Every error says what is wrong and how to fix
it. No lorem ipsum, no "John Doe", no invented statistics beyond this story.

## 7. What every screen shows

- The requirements in its inventory entry: every region and field the PRD names.
- One primary action. Status chips with icon + label. Figures as links (drill-down, FR-UX-04).
- Help: an `fy-info` button beside labels whose meaning is not obvious (FR-UX-30).
- AI only where the PRD puts it, violet, labelled, with its sources; never on buttons.
- Records use the record anatomy (PRD §7.3): header (number, status chip, contact, key amounts,
  primary actions, "⋯"), tabs (Details · Lines · Ledger impact · Attachments · Activity · Related),
  and the right panel (checks, AI suggestions) as the `fy-split` aside.
- The populated state. Show a state the inventory lists (locked period, can't post, empty, error)
  inline where it matters, as a banner, chip or empty card; do not make extra screens for states.

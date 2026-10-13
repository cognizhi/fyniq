# Nemura design system: MASTER

How Nemura looks and behaves (PRD-002 §19, NM-UX-01…05). Tokens are in `tokens.css`; the shared
component structure is `docs/design/screens/kit/ui.css`, and Nemura's own layer is
`docs/design/screens/kit/nemura.css` (until `apps/nemura-web` exists). Anything this file does not
say follows Fyniq's MASTER (`docs/design-system/fyniq/MASTER.md`) for behaviour: accessibility,
device classes, motion rules, numbers and money, help, AI marking.

## 1. Direction: professional, modern, with depth

Nemura is for everyone in the company, most of whom are not finance people. It should feel calm,
trustworthy and current, and easy enough that nobody needs training.

| Principle | In practice |
|---|---|
| **Do the common thing in one tap** | My home opens with large tiles for the six things people do most; each opens the task, not a menu |
| **Depth, not decoration** | Surfaces sit at clear levels: the page, elevated cards, raised tiles and buttons, paper lifted off the page. Layered soft shadows and a one-pixel top highlight do this; no textures, no glass blur behind text |
| **Cool and professional** | Slate surfaces, deep navy text and navigation, one ocean-blue action colour, soft tints for tiles |
| **Plain words** | "Apply for leave", "Your payslip", "Ask for a letter"; never "Submit absence request" |
| **No financial clutter** | People pages never show ledger, bank, revenue or cash figures (NM-ROLE-02) |

Rejected on purpose: warm cream and ink outlines (the first concept, 13 Oct 2026); Inter, Geist,
DM Sans, Space Grotesk, Plus Jakarta Sans and the other defaults most AI-built interfaces use;
glassmorphism behind text; dark-only themes; neon gradients.

## 2. Colour roles

| Role | Token | Use |
|---|---|---|
| Page and surfaces | `--background`, `--surface-1…3` | Cool slate page, white cards, slate panels and wells |
| Text | `--text-primary` (deep navy), `--text-secondary`, `--text-muted` | Content, labels, help |
| Action | `--primary` (ocean blue, hue 245) | Primary buttons, links, selection, focus ring. One primary action per screen |
| Navigation | `--nav-bg` → `--nav-bg-2` (navy gradient), `--nav-fg`, `--nav-active-bg` | The rail and tab bar; the current page is a raised white pill (a blue pill in dark mode) |
| Tiles | `--tile-marigold`, `--tile-mint`, `--tile-sky`, `--tile-blush`, `--tile-sand` with `--tile-*-deep` and `--tile-fg`, `--tile-icon-fg` | Soft tints for action tiles and leave types; the deep shade for icon discs, dots and progress |
| Status | `--status-*` | Chips and banners, always icon + label, as in Fyniq |
| AI | `--ai*` (violet) | Only AI content, always with the AI badge; the same rule in Fyniq and Nemura. The action blue is kept far from it |

**Leave types** keep one tint each: annual = sky, sick = blush, family (maternity, paternity, shared
parental, childcare) = mint, public holiday = marigold, unpaid = slate. The type's name always sits
next to its dot (`.nm-type`).

**Tiles by meaning**: marigold = the main thing to do now (Apply for leave); mint = pay; sky = letters
and documents; blush = help and support; sand = everything else.

## 3. Type

| Family | Token | Use |
|---|---|---|
| **Funnel Display** 600–800 | `--font-display` | Page titles, greetings, tile titles, big figures, table headers |
| **Funnel Sans** 400–800 | `--font-ui` | Everything else; tabular figures for numbers (`tnum`) |
| **Spline Sans Mono** 400–500 | `--font-code` | IDs, references, merge fields |

Scale: 15 px body, 16 px forms and reading, 19 px section titles, 28/34 px page titles (compact /
expanded), 32/42 px greetings, 52 px hero figures. Weights stay at 600–700 for headings: confident,
not loud. All SIL OFL.

## 4. Depth and shape

- **Levels**: page (`--background`) → card (`--surface-1`, 1 px `--border`, `--shadow-2`, top
  `--highlight`) → raised (tiles, primary buttons, the rail's current page, `--shadow-raised`) →
  overlay (dialogs, paper, `--shadow-3`). Wells (board columns, segmented controls, progress tracks,
  inputs) are inset with a soft inner shadow.
- **Tiles**: a gradient from the tint towards `--tile-glow` (white in light, a lifted navy in dark),
  a hairline border in the deep shade, and a soft shadow tinted with it; the icon sits on a glossy
  disc in the deep shade. Hover lifts the tile 3 px.
- **Primary buttons**: a vertical gradient on ocean blue, a darker 1 px edge, the top highlight and
  a blue-tinted shadow; hover lifts 1 px, press sinks into an inner shadow.
- **Radii**: 8 px chips and small parts, 12 px buttons, inputs and inner cards, 18 px cards and
  tiles, pills for filters and counters.
- Gradients never sit behind body text except a tile's tint, which only moves towards
  `--tile-glow`, so text contrast never drops below the checked value.

## 5. Layout and navigation

- Same device classes and five frames as Fyniq (FR-UX-37). Content max width 1200 px.
- **Rail** (medium) and **sidebar** (expanded): navy gradient, groups "For you" and "People team",
  the current page a raised pill. **Tab bar** (compact): the same navy, a raised pill behind the
  current icon. Employees: Home, Leave, Pay, Requests, More; the people team: People home, People,
  Leave, Letters, More.
- **Header**: white with a hairline and soft shadow; brand, organisation, search ("Search people,
  letters, policies…"), the **Finance | People** switch (`.nm-workspace`, only for people with both),
  notifications, Ask, avatar.
- **Pages**: a title, one primary action on the right (or in the bottom action bar on phones), then
  cards. Self-service pages use `.nm-hero` and `.nm-tiles`.

## 6. Components (Nemura-only, `nm-`)

| Component | Class | Notes |
|---|---|---|
| Greeting | `.nm-hero`, `__title`, `__sub` | One sentence that says what matters today |
| Action tiles | `.nm-tiles` (`--4` for four), `.nm-tile--{marigold,mint,sky,blush,sand}`, `__icon`, `__title`, `__meta`, `__count` | The whole tile is the target |
| Icon disc | `.nm-icon--{marigold,mint,sky,blush}` | The tile's glossy disc on its own, for list rows and cards |
| Big figures | `.nm-stat`, `__value`, `__label`, `__sub`; `.nm-stats` | Display font, tabular figures |
| Balance bar | `.nm-bar`, `__used`, `__booked` | Inset track; always with numbers beside it |
| Leave type | `.nm-type--{annual,sick,family,holiday,unpaid}` | Dot + word |
| Month calendar | `.nm-cal`, `__dow`, `__day`, `__n`, `__ev--{type}` | Events become bars on phones; a list below repeats them in words |
| Board | `.nm-board`, `.nm-col`, `.nm-person` | Inset columns, raised cards; every move also has a menu action (no drag-only) |
| Goal ring | `.nm-ring` with `style="--value: 60"`, `--good`, `--warning` | Always with the number inside |
| Paper | `.nm-paper`, `__head`, `__org`, `__sign`; `.nm-field` | Letters, payslips and templates, lifted off the page; merge fields in blue |
| Request track | `.nm-track` with `is-done`, `is-now` | Always with the step's name |
| Pills | `.nm-pills`, `.nm-pill` | Filters; `aria-pressed`; the pressed pill is navy |
| Workspace switch | `.nm-workspace` | Two links, `aria-current` |
| Sign-in | `.nm-signin`, `__art`, `__main`, `__card`, `__big` | Navy art panel with a blue glow on expanded screens |

Shared Fyniq components (`fy-`) keep their behaviour; Nemura restyles the shell, buttons, inputs,
cards, chips, tabs, avatars and tables.

## 7. Motion

Fyniq's rules (MASTER §9, FR-UX-38): 120/180/240 ms, `transform` and `opacity` only, off with the
organisation switch or reduced motion. Nemura adds the lift on hover and the sink on press for
tiles, cards on the board and buttons, and nothing else.

## 8. Accessibility

WCAG 2.2 AA as Fyniq; `make check-tokens` validates Nemura's tokens in four themes, including the
rail and tab bar (both gradient ends), the current-page pill, every tile tint with its text and every
icon disc with its icon. Targets are 44 px. Colour never carries meaning alone: leave types have
words, statuses have icons and labels, the calendar repeats events as a list for screen readers.
Shadows and highlights are decoration only; boundaries that matter use `--border-strong`.

## 9. Anti-patterns

- Financial figures on any people page (NM-ROLE-02).
- A tile without a verb, or more than six tiles on My home.
- Violet on anything that is not AI content; blue that drifts towards violet.
- Shadows on everything: the page and table rows stay flat; depth marks levels, not decoration.
- Blur or glass behind text; gradients that darken under dark text.
- Drag-only moves on the hiring board or calendar.
- Default fonts (§1).

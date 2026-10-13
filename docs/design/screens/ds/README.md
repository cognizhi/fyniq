# Fyniq

*Intelligence behind every number.* Fyniq is the accounting, compliance, reporting and planning
platform of Cognizhi, a Singapore start-up. This design system is how its screens look and
behave. The source of truth is the repository: `docs/design-system/fyniq/MASTER.md` (rules),
`docs/design-system/fyniq/tokens.css` (tokens, validated for contrast in all four themes) and
`docs/design/screens/` (the screen kit and every screen). This page is generated from them.

## Direction: calm confidence with a spark

Serious about numbers, quiet in chrome. Personality comes from colour, motion and craft, never
from decoration. A minimal Swiss base (grid, clear hierarchy, disciplined white space) carries
data-dense working views: compact cards, sticky table headers, figures that line up.

- **One primary action per view.** Everything else is secondary (outline), ghost or in "⋯".
- **Every figure opens to its source.** Tiles, totals and chart marks are links: breakdown, ledger
  lines, source document, history.
- **Plain words.** Basic mode says "Money in" and "Category"; Expert mode adds debits, credits and
  account codes. Same data, same actions, same safety information.

## Colour roles

Components use the semantic tokens only, never raw values.

| Role | Tokens | Use | Never |
|---|---|---|---|
| Ground and surfaces | `background`, `surface-1` (cards, tables), `surface-2` (subtle panels, table headers, sidebar), `surface-3` (hover, selection) | Light is warm paper; dark is layered "ink" blue-black, never pure black | — |
| Text | `text-primary`, `text-secondary`, `text-muted` | Figures and content; labels; help and captions | Series or status colours for body text |
| Brand teal | `primary`, `primary-hover`, `primary-subtle`, `link`, `ring` | The one primary button, links, selected navigation, focus rings | AI content; data series |
| **AI violet** | `ai`, `ai-subtle`, `ai-subtle-foreground`, `ai-border` | **Only** content an AI produced: assistant messages, proposals, suggestions, the AI badge | Buttons, links, anything a person made |
| Status | `status-good`, `status-warning`, `status-serious`, `status-critical`, `status-neutral` and their `-subtle` grounds | Chips, banners and check results, always with an icon and a label | Decoration; data series |
| Destructive | `destructive` | Void, revoke and other destructive actions, set apart from the primary | Field errors (use `status-critical`) |
| Charts | `chart-1` … `chart-8`, `chart-other`, `seq-*`, `div-*`, `viz-status-*` | Data marks only, in fixed order, following the entity not its rank | Text, chrome, the brand accent |

The violet-to-teal gradient appears in exactly two places: the band on the Home pulse card and the
header of the AI panel.

**Themes.** Light (default), Dark, High contrast (7:1 text) and High contrast dark. Each theme has
its own chosen steps; dark is not an inversion.

## Status chips

Workbook symbols become an icon plus a label. The symbols themselves appear only in exports and
plain-text email.

| Meaning | Chip | Tokens |
|---|---|---|
| OK | Lucide `circle-check` + "OK" | `status-good` on `status-good-subtle` |
| Included, please review | `triangle-alert` + "Review" | `status-warning` |
| Cannot post until fixed | `circle-x` + "Can't post" | `status-critical` |
| Due or overdue | `clock-alert` + "Due in 3 days" | `status-serious` |
| Draft | `pencil-line` + "Draft" | `status-neutral` |
| Locked period | `lock` + "Locked" | `status-neutral` |
| Not assessed | `triangle-alert` + "Not assessed" | `status-warning` |

## Type

**Inter** for the interface, with tabular figures in every table, total, tile and axis, and
**JetBrains Mono** for references, IDs and account codes. The product self-hosts both; design
canvases load them from Google Fonts.

| Style | Size / line | Weight | Use |
|---|---|---|---|
| caption | 12 / 16 | 500 | Captions, axis labels, badges |
| table-compact | 13 / 18 | 400 | Compact tables |
| body | 14 / 21 | 400 | Interface text, comfortable tables |
| reading | 16 / 24 | 400 | Forms, reading text, all text on phones |
| section-title | 18 / 24 | 600 | Section titles |
| page-title | 22 / 28 | 600 | Page titles |
| stat | 28 / 34 | 600 | Stat tiles |
| pulse | 36 / 40 | 600 | Home pulse figures |

Numbers are right-aligned with thousands separators and the currency in the column header.
Negatives show as "−1,234.56" or "(1,234.56)"; copied values use an ASCII minus.

## Layout and devices

A 4 px grid (`space-1` … `space-8`), radii `radius-s` (chips, inputs), `radius-m` (buttons),
`radius-l` (cards, dialogs), and three elevations (`shadow-1` resting, `shadow-2` raised,
`shadow-3` overlays). Every screen is designed for three device classes and five reference frames:

| Class | Width | Frames | Navigation |
|---|---|---|---|
| Compact | < 600 px | Phone 390 × 844; foldable folded 344 × 882 | Top bar and a bottom bar of five labelled destinations |
| Medium | 600–1023 px | Foldable open 690 × 840; tablet 834 × 1194 | Navigation rail; list and detail side by side |
| Expanded | ≥ 1024 px | Desktop 1440 × 900 | Sidebar (240 px, 64 px collapsed); docked AI panel |

Touch targets are at least 44 px; pointer targets at least 24 px. On a device that spans a hinge,
two panes split at the hinge and nothing straddles it.

## Motion

Minimal and purposeful: page entrances (fade and 8 px rise, 180 ms), panels sliding from their
edge (240 ms), dialogs scaling from 0.98, list rows in a 30 ms stagger (8 rows at most),
skeleton sheen while loading, chip and count cross-fades, the Home pulse ticker and the "Books
closed" check. `transform` and `opacity` only. The Administrator can switch motion off for the
organisation; users can reduce it; the operating system's reduced-motion setting always wins.

## Icons

Lucide only, 16 / 20 / 24 px with a 1.5 px stroke. Icons beside text are decorative; icon-only
buttons carry an accessible name and a tooltip. No emoji as icons.

## Accessibility

WCAG 2.2 AA: text 4.5:1 (7:1 in high contrast), boundaries and focus rings 3:1, a visible 2 px
`ring` focus outline, focus never hidden by sticky bars or panels, keyboard alternatives for every
drag, accessible sign-in (paste, password managers, passkeys), and colour never the only cue.

## Never

Raw colours in components; violet on anything not AI-made; emoji icons; colour as the only signal;
optimistic money; dual-axis charts; modals for navigation; Basic mode silently hiding a value that
is set on a record.

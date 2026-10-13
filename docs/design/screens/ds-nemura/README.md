# Nemura

Nemura is the people platform of Cognizhi, a Singapore start-up: people records, leave, payroll,
payslips, letters and requests, and later hiring, performance and onboarding (PRD-002). It runs on
its own or linked to Fyniq, Cognizhi's accounting platform. This design system is how its screens
look and behave. The source of truth is the repository: `docs/design-system/nemura/MASTER.md`
(rules), `docs/design-system/nemura/tokens.css` (tokens, validated for contrast in all four
themes) and `docs/design/screens/` (the screen kit and every screen). This page is generated from
them.

## Direction: professional, modern, with depth

For everyone in the company, most of whom are not finance people: calm, trustworthy, current.

- **Do the common thing in one tap.** My home opens with large tiles for the six things people do
  most; each opens the task.
- **Depth, not decoration.** Clear levels: the page, elevated cards, raised tiles and buttons, paper
  lifted off the page. Layered soft shadows and a one-pixel top highlight; no glass behind text.
- **Cool and professional.** Slate surfaces, deep navy text and navigation, one ocean-blue action
  colour, soft tints for tiles.
- **No financial clutter.** People pages never show ledger, bank, revenue or cash figures.

## Colour roles

| Role | Tokens | Use | Never |
|---|---|---|---|
| Page and surfaces | `background`, `surface-1`, `surface-2`, `surface-3` | Slate page, white cards, slate wells | Tinted pages |
| Text | `text-primary`, `text-secondary`, `text-muted` | Deep navy content, labels, help | Grey on slate below 4.5:1 |
| Ocean blue | `primary`, `primary-hover`, `primary-subtle`, `link`, `ring` | The one primary action, links, focus | Status; AI |
| Navigation | `nav-bg`, `nav-bg-2`, `nav-fg`, `nav-muted`, `nav-active-bg`, `nav-active-fg` | The navy rail and tab bar; the raised pill for the current page | Page content |
| Tiles | `tile-marigold`, `tile-mint`, `tile-sky`, `tile-blush`, `tile-sand` (+ `-deep`), `tile-fg`, `tile-icon-fg` | Action tiles and leave types (annual sky, sick blush, family mint, holiday marigold) | Meaning by colour alone |
| Status | `status-*` | Chips and banners with an icon and a label | Decoration |
| **AI violet** | `ai`, `ai-subtle`, `ai-subtle-foreground`, `ai-border` | Only AI content, always with the AI badge | Buttons, links |

## Type

**Funnel Display** (600–800) for titles, greetings, tile titles and big figures; **Funnel Sans**
(400–800, tabular figures) for everything else; **Spline Sans Mono** for IDs and merge fields. All
SIL OFL, from Google Fonts. Not Inter, Geist or the other defaults.

## Depth and shape

Cards: white, 1 px border, `shadow-2` and a top highlight. Raised (tiles, primary buttons, the
current page): `shadow-raised`. Overlays and paper: `shadow-3`. Tiles: a tint that brightens towards
the top-left, a hairline border and a tinted shadow, with a glossy icon disc. Radii 8 / 12 / 18 px.

## Components

Nemura reuses Fyniq's component structure (`fy-` classes) restyled, and adds `nm-` components (icon discs `nm-icon--*` too):
greeting (`nm-hero`), action tiles (`nm-tiles`, `nm-tile--marigold|mint|sky|blush|sand`), big
figures (`nm-stat`), balance bar (`nm-bar`), leave type (`nm-type--annual|sick|family|holiday|unpaid`),
month calendar (`nm-cal`), board (`nm-board`, `nm-col`, `nm-person`), goal ring (`nm-ring`),
paper for letters and payslips (`nm-paper`, `nm-field`), request track (`nm-track`), pills
(`nm-pills`, `nm-pill`), the Finance | People switch (`nm-workspace`) and sign-in (`nm-signin`).

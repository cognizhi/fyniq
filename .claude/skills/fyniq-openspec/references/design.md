# Designing screens in Claude Design

Every screen a PRD implies is designed before it is built, at five frames in light and dark, on a
Claude Design canvas. Load **fyniq-design-system** for the visual rules and consult uipro
(`ui-ux-pro-max`) for any new pattern; record what you adopt in `MASTER.md`.

## The pieces

| Path | What |
|---|---|
| `docs/design/screens/inventory.json` | Every screen: id, file, title, canvas, module, route, kind, phase, roles, mode, requirement IDs, purpose, content, states |
| `docs/design/screens/AUTHORING.md` | How to write a screen: fragment format, canvas rules, responsive classes, components, motion, the sample company |
| `docs/design/screens/sample-data.json` | The sample company's register: chart of accounts, contacts, people, every document and AI proposal number, headline figures. `build.py check` enforces the codes, numbers, dates, times and masked IDs; figures are copied by hand |
| `docs/design/screens/src/<File>.html` | One fragment per screen (content only; the shell is added by the build) |
| `docs/design/screens/kit/ui.css` | The component layer (hand-written); `kit/fyniq-ui.css` is generated from it and `tokens.css` |
| `docs/design/screens/kit/lucide-icon-nodes.json` | Lucide 1.48.0 icons (ISC), expanded inline by the build |
| `docs/design/screens/ds/` | The Fyniq design system's README and cover (sources) |
| `docs/design/screens/canvases.json` | The published canvases: area id → Claude Design URL, and the design system URL |
| `docs/design/screens/build.py` | `kit`, `check`, `canvas`, `ds`, `status` |
| `docs/design/screens/out/` | Build output, ignored by git |

## Frames, classes and themes

| Frame | CSS px | Class |
|---|---|---|
| Desktop | 1440 × 900 | expanded |
| Tablet | 834 × 1194 | medium |
| Foldable open | 690 × 840 | medium |
| Phone | 390 × 844 | compact |
| Foldable folded | 344 × 882 | compact |

Each screen is one fragment, responsive through container queries on `.fy-device`, so the same
markup renders all five frames. The desktop-light artboard is the screen's source component
(`<File>.dc.html`); the other nine import it with `<dc-import>` and a theme. The source's Tweaks
panel switches theme (light, dark, high contrast, high contrast dark), mode (Basic, Expert),
motion (on, off, reduce) and density. Frames grow to the `heights` the fragment declares, so long
pages show in full.

## Recipe

1. **Inventory**: add or update the screen's row in `inventory.json` (requirement IDs must exist in
   the PRD; `build.py check` verifies them). Keep each canvas at 17 screens or fewer (the canvas
   loads about 199 files reliably; a screen takes 10).
2. **Fragment**: write `src/<File>.html` following `AUTHORING.md`; `src/Home.html` is the reference.
   Take every account, document number, person and figure from `sample-data.json`; register a new
   document there before a screen uses it. The screens show one moment, 13 Oct 2026 09:12.
3. **Lint and size**: `python3 docs/design/screens/build.py check <File>` → 0 errors; then
   `build.py heights <File> --write` sets the frame heights from the rendered page, and
   `build.py shots <File> desktop-light fold-closed-dark` shows the widest and narrowest frames
   (344 px is where long button labels and chips overflow).
4. **Build**: `python3 docs/design/screens/build.py canvas <area> --ds <design system URL>` (URLs in
   `canvases.json`). It writes `out/<area>/project/`: `canvas.json`, `kit/fyniq-ui.css`, the source
   and the nine frame artboards per screen, one canvas page per module.
5. **Publish** with the Artifact tool:
   - First time for an area: create the canvas (Artifact `publish` with the Design type's
     `type_url` from `canvases.json`, `title` = the canvas title from `inventory.json`,
     `auto_open: "after_first_write"`, no files), and record its URL in `canvases.json`.
   - Then one publish: `url` = the canvas, `root` = `docs/design/screens/out/<area>`,
     `file_path` = `…/out/<area>/project/canvas.json`, `files` = every other file under `project/`
     plus `"project/ds/fyniq/tokens.json": {"artifact": "<design system URL>", "path": "project/tokens.json"}`
     (installs the design system so the colour pickers show Fyniq's tokens).
   - Updating later: someone may have edited the canvas by hand. Read `project/canvas.json` first,
     publish only the changed artboards (the screen's source and its nine frames) and send
     `canvas.json` only when artboards were added or removed. A refused publish names the file to
     re-read: merge, then publish again. A refusal saying you have not viewed the latest version
     means the canvas was saved in the page: `read` the canvas (no `path`), then publish again.
   - A kit change (`kit/ui.css`) reaches every canvas only when `project/kit/fyniq-ui.css` is
     republished to each URL in `canvases.json` (rebuild each canvas first).
6. **Link back**: the inventory row's requirement IDs tie the screen to the specs; mention the
   canvas URL in the pull request.

## Commands (`python3 docs/design/screens/build.py …`)

| Command | Does |
|---|---|
| `kit` (`make design-kit`) | Regenerates `kit/fyniq-ui.css` from `tokens.css` and `kit/ui.css` |
| `check [File…]` (`make design-check`) | Lints fragments: format, tokens only, icons, inventory, stale kit, and the sample data (account names, document owners, proposal numbers, weekdays, the 09:12 clock, masked IDs) |
| `status` (`make design-status`) | Screens written per canvas |
| `ds` | Builds the Fyniq design-system files in `out/ds/project/` |
| `canvas <area> --ds <URL>` | Builds `out/<area>/project/`, ready to publish |
| `files <area> [--list]` | The `files` value for the publish call |
| `preview [File…]` | Each frame as a standalone page in `out/preview/<File>/` (local only) |
| `shots <File> [frame-theme…]` | PNG screenshots of those pages with headless Chrome (local only) |
| `heights <File>… [--write]` | Measures content height per class and sets the fragment's `heights` |

## Rules of the canvas format (the build and the lint enforce them)

Artboards are `.dc.html` files with `<script src="./support.js"></script>` in the head, markup in
`<x-dc>`, page basics in `<helmet>` (only the Google Fonts link and `body{margin:0}`), and a
`<script type="text/x-dc" data-dc-script data-props='…'>` class. Every element closed, every
attribute quoted, `{{…}}` only for template holes, no handlers or scripts in markup, no emoji, no
iframes, icons as inline SVG. Stylesheets under `project/` load by relative path
(`kit/fyniq-ui.css`). Do not render, screenshot or re-read a canvas to "verify" it: the user
reviews it in Claude Design.

## Nemura (PRD-002)

Nemura's canvases (`nemura-me`, `nemura-team`, `nemura-setup`) have `"product": "nemura"` in `inventory.json`:
they build with `kit/nemura-ui.css`, Funnel Display, Funnel Sans and Spline Sans Mono, and Nemura's shell
(`persona`, `integrated` settings). Their design system is its own artifact (`design_system_nemura` in
`canvases.json`; `build.py ds nemura`, sources in `ds-nemura/`). `make check-tokens` validates both token
files. Nemura's register entries are under `nemura` in `sample-data.json`, same clock.

## Design-system artifact

The **Fyniq** design system (URL in `canvases.json`) holds `tokens.json` (60 colours × 4 themes,
spacing, radii, shadows, type), the brand book `README.md` and the cover. Rebuild it after a
token or MASTER change: `build.py ds`, then publish `out/ds/project/` to the same URL (read its
`project/design-system.json` first and keep its keys; send the index last).

# Threat model: the company logo and brand

| | |
|---|---|
| Feature and requirement IDs | The organisation's logo for light and dark backgrounds, its icon and accent colour, kept as versions: FR-ORG-09, G4-099 (PRD-001 rev 0.7, rev 0.8, rev 0.200); `p1-organisation-chart` task 2.4 |
| Author, reviewer, date | Claude Code for the founders, 7 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Logo files (general, public in documents), brand versions |
| Who acts | The Owner, the Administrator or the Controller uploads; every member reads |

## 1. What we are building

An upload (`PUT /v1/organisation/brand/files/{kind}`, multipart) is read in full up to its limit, typed by its content,
cleaned (`internal/domain/brand`: an SVG rewritten from its safe elements and attributes, a PNG decoded and encoded
again), scanned for malware, stored in the blob store under its SHA-256, and recorded as the next brand version. Members
read the version in force and its files as base64 JSON; the web app shows them only as images. Trust boundaries:
uploader → API (the file is hostile input), API → blob store, API → viewers' browsers (the file is shown to everyone).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | uploader → API | Elevation: an SVG with a script or event handler runs in a viewer's browser (stored XSS) | Session theft of every member | Medium |
| T2 | uploader → API | Information disclosure: an SVG references an outside font or image, so each view calls a tracker | Viewers' IPs and timing leak | Medium |
| T3 | uploader → API | Denial of service: an entity bomb, a deep or huge SVG, or a PNG decompression bomb | The API's memory is exhausted | Medium |
| T4 | uploader → API | Information disclosure: an external entity reads a local file | Server files leak | Low |
| T5 | stored file | Tampering: the blob changes after it was recorded | A different logo, or a hostile one, is served | Low |
| T6 | brand | Repudiation, or a lost logo: a change overwrites history, so an issued document's logo is gone | A reprint differs; no trail | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Only SVG shapes, gradients, clips, masks and text, with an attribute allowlist; every `on*` handler, script, style sheet and foreign object removed; the result written again from tokens; the web app shows files only as images (`img` from a data URL), never inline | `TestAHostileSVGLogoIsMadeSafe` (V1.3.4) |
| T2 | Every `href` and `url()` that is not `#id` removed, in attributes and in style declarations; embedded images and fonts removed | `TestAHostileSVGLogoIsMadeSafe` |
| T3 | Read at most 512 KB of SVG or 2 MB of PNG; 20,000 elements and 64 levels at most; a PNG's size read from its header (at most 4,096 pixels a side) before it is decoded | `TestASVSWithEntitiesOrNoShapeIsRefused`, `TestAPNGLogoIsReadAndWrittenAgain` (V1.5.1, V5.2.1, V5.2.6) |
| T4 | A document type or entity declaration refuses the file; the decoder knows only XML's own entities | `TestASVSWithEntitiesOrNoShapeIsRefused`, `TestTheLogoAndBrand` |
| T5 | Blobs are written once under their content's SHA-256 (`Create`), and a read checks the hash before answering | `TestTheLogoAndBrand` |
| T6 | `brand_version` is insert-only and numbered by the database (FY282, FY283), at the version read; each version is audited | `TestTheLogoAndBrand` |

## 4. What remains

- The PNG rasterised from an SVG (the render service, Typst) and the logo on documents, emails, reports, the sign-in page
  and the portal arrive with the rest of task 2.4.

---
key: settings.brand
type: task
title: Upload the company logo and choose the accent colour
summary: The logo for light and dark backgrounds and the square icon, what Fyniq cleans from an SVG, versions, and the accent colour of documents and emails
sources:
  - docs/user-guide/logo-and-brand.md sha256:6ee20cf84e77146cec223f275fbf188d1d475c177582d914a29e340711e31c76
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
aliases:
  - logo
  - company logo
  - brand
  - accent colour
  - upload logo
  - svg logo
  - app icon
---

# Upload the company logo and choose the accent colour

## What the page holds

- **Settings → Logo and brand** holds the company's logo for light and for dark backgrounds, a square icon and the accent colour of documents and emails; the Owner, the Administrator and the Controller change them, and everyone sees the logo in the app's header.
- Each change is a new version and takes effect at once; documents already issued keep the logo they were issued with ([[settings.templates]]).

(src: docs/user-guide/logo-and-brand.md#what-it-is)

## Upload the logo

- **Upload** for light backgrounds, dark backgrounds or the square icon: an SVG, or a PNG at least 512 pixels wide (an icon: square, an SVG or a PNG at least 192 pixels wide); confirm it's you if asked.
- Without a dark-background logo, dark screens show the light one on a white tile. **Replace** swaps a file; **Remove** takes it out of the next version.

(src: docs/user-guide/logo-and-brand.md#upload-your-logo)

## What Fyniq cleans from an SVG

- Only shapes, colours, gradients and text stay; scripts and event handlers, links to anything outside the file (web fonts, remote images), embedded images and fonts, and document type or entity declarations are removed.
- The page says what was removed, marked **Cleaned**; a PNG is written again without its hidden metadata.

(src: docs/user-guide/logo-and-brand.md#what-fyniq-cleans-from-an-svg)

## Accent colour and problems

- **Accent colour**: one of the options, or **The default**; it colours documents and emails only, the app keeps its own colours, and each option reads clearly against white ([[settings.organisation]]).
- "Not an SVG or a PNG Fyniq reads.": export the logo again. "The file is too large, too small or too complex.": an SVG is at most 512 KB, a PNG at most 2 MB and at least 512 pixels wide (an icon square, at least 192).

(src: docs/user-guide/logo-and-brand.md#choose-the-accent-colour) (src: docs/user-guide/logo-and-brand.md#common-problems) (src: docs/user-guide/logo-and-brand.md#related)

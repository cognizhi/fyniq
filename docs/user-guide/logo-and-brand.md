---
key: settings.brand
title: Logo and brand
mode: basic
roles: [owner, admin, controller]
requirements: [FR-ORG-09]
status: draft
---

# Logo and brand

## What it is

**Settings → Logo and brand** holds your company's logo, for light and for dark backgrounds, a square
icon, and the accent colour of your documents and emails. The Owner, the Administrator and the
Controller change them; everyone in the company sees the logo in the app's header.

Each change is saved as a new version and takes effect at once. Documents already issued keep the
logo they were issued with.

## Upload your logo

1. Under **Logo files**, select **Upload** for the logo for light backgrounds, for dark backgrounds,
   or for the square icon.
2. Choose an SVG, or a PNG at least 512 pixels wide. An icon is square: an SVG, or a PNG at least
   192 pixels wide.
3. Confirm it's you if Fyniq asks.

Until you add a logo for dark backgrounds, dark screens show the light one on a white tile.
**Replace** swaps a file; **Remove** takes it out of the next version.

## Your logo on documents

When you upload the logo for light backgrounds, Fyniq also draws it as your documents show it: a
PNG 192 pixels high, on a transparent background. **On documents and emails** shows that drawing.

Quotes, invoices, statements and remittance advice show it, and so does the page your customer
opens from a quote's or invoice's link. A document always shows the logo and accent that were in
force when it was issued, so changing your logo later never changes a document you already sent.

## What Fyniq cleans from an SVG

An SVG can carry more than shapes. Before it is used, Fyniq keeps only the shapes, colours,
gradients and text, and removes:

- scripts and event handlers, which could run code;
- links to anything outside the file, such as a web font or an image on another site;
- embedded images and fonts, and any document type or entity declarations.

The page says what was removed, marked **Cleaned**; the logo usually looks the same. A PNG is
written again without its hidden metadata.

## Choose the accent colour

Under **Accent colour**, choose one of the options, or **The default**. The accent colours your
documents and emails only; the app keeps its own colours. Each option reads clearly against white.
Documents laid out with Fyniq's own template take your accent; a template you published keeps the
colour chosen in it.

## Common problems

- **"Not an SVG or a PNG Fyniq reads."** Export the logo again as an SVG or a PNG.
- **"The file is too large, too small or too complex."** An SVG is at most 512 KB; a PNG at most
  2 MB, and at least 512 pixels wide (an icon, square and at least 192).
- **"The logo cannot be drawn for documents and emails."** The logo is too detailed to draw, or
  uses something the drawing does not support. Upload a simpler SVG, or the logo as a PNG.

## Related

- [Organisation settings](organisation-settings.md)
- [Document and email templates](document-templates.md)

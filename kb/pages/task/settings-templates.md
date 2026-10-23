---
key: settings.templates
type: task
title: Design documents with templates
summary: How templates set the look of invoices, quotes and other documents; start, edit, publish, roll back, choose the default, move with a bundle
sources:
  - docs/user-guide/document-templates.md sha256:f9b81d0e45521ee92e8a11bb7647a1dbda19e86feae1f35a1a0eed7927da0758
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-23
aliases:
  - invoice template
  - document template
  - invoice design
  - change the look of invoices
  - template version
  - tax invoice layout
---

# Design documents with templates

## What a template is

- A template decides how a document looks (invoice, quote, credit note, receipt, customer statement, reminder letter, remittance advice, purchase order) and the email that carries it; it holds a style, a theme, the blocks shown and texts with fields, never code.
- Each template has a **draft** you save as often as you like and **published versions** that never change; documents render with a published version and keep it, so a reprint is identical.
- Until a type has a published template, the **Classic** starter renders it.

(src: docs/user-guide/document-templates.md#what-a-template-is)

## Start a template

- **Settings → Templates**, then **New template** or **Use** under a style in the starter gallery (Classic, Band, Minimal, Compact); choose the document, document or email, and a name, then **Make the draft**.
- Nothing changes on documents until it is published.

(src: docs/user-guide/document-templates.md#start-a-template)

## Edit a template

- Style, theme and page: logo shown or not, an accent colour with a contrast of at least 4.5:1 against white, a font, A4 or Letter, margins of 10 to 40 mm.
- Blocks and texts: show or hide blocks and write their texts; on invoices and credit notes the blocks carrying IRAS particulars are locked, and "Tax Invoice" is set for GST-registered companies.
- **Insert field** adds fields, with formats for dates (23 October 2026) and amounts (S$1,234.50); texts hold words and fields only.
- The live preview uses the company's own details and a made-up customer, and names a particular missing from **Settings → Organisation**. **Save draft** keeps the changes.

(src: docs/user-guide/document-templates.md#edit-a-template)

## Publish a version

- Save the draft, write what changed if you like, then **Publish**; for invoices and credit notes test documents are rendered first and publishing is refused if one lacks an IRAS particular.
- An Owner or a Controller publishes: alone in Founder mode (it joins the weekly review), otherwise **Ask for approval** and another Owner or Controller who did not save the draft approves ([[approvals.decide]]).

(src: docs/user-guide/document-templates.md#publish-a-version)

## Roll back

- **Versions → Roll back** publishes an earlier version again as the next version; the draft stays as it is.

(src: docs/user-guide/document-templates.md#roll-back)

## Choose the default

- An Owner or a Controller selects **Change default** beside a document type and picks a published template or the Classic starter; no second approval, as each was approved when published.
- A customer's or supplier's own template, chosen with **Templates** next to the contact in **Contacts** ([[contacts.manage]]), comes before the default.

(src: docs/user-guide/document-templates.md#choose-the-default)

## Move templates with a bundle

- In Expert mode, **Template bundle** exports templates as one file (the version in force, or a never-published draft, with a fingerprint); **Import bundle** brings it in as drafts, refused if a template changed after export.

(src: docs/user-guide/document-templates.md#move-templates-with-a-bundle)

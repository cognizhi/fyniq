---
key: settings.templates
title: Design your documents with templates
mode: basic
roles: [owner, controller, accountant, ar_specialist, ap_specialist, auditor]
requirements: [FR-TPL-01, FR-TPL-02, FR-TPL-03, FR-TPL-04, FR-TPL-05, FR-TPL-07, FR-AR-05]
---

# Design your documents with templates

## What a template is

A template decides how a document looks: an invoice, a quote, a credit note, a receipt, a customer
statement, a reminder letter, a remittance advice or a purchase order, and the email that carries it.
A template is never code: it holds a style, a theme, the blocks the document shows, and texts that use
fields such as the customer's name or the due date.

Each template has a **draft**, which you change and save as often as you like, and **published
versions**, which never change. Documents always render with a published version, and each document
keeps the version it was issued with, so a reprint is identical.

Until your company publishes a template of a document type, Fyniq renders it with the **Classic**
starter.

## Start a template

1. Open **Settings → Templates**.
2. Select **New template**, or **Use** under a style in the starter gallery.
3. Choose the document, whether it is the document itself or its email, a name and a style: Classic,
   Band, Minimal or Compact.
4. Select **Make the draft**. Fyniq opens the new draft; nothing changes on your documents yet.

## Edit a template

- **Style**: change the look at any time; your texts and blocks stay as they are.
- **Theme and page**: show your logo or not, choose an accent colour and a font, the paper (A4 or
  Letter) and the margins (10 to 40 mm). An accent colour must have a contrast of at least 4.5:1 against
  white, so that text in it is easy to read.
- **Blocks and texts**: show or hide each block and write its texts. On invoices and credit notes the
  blocks that carry the particulars IRAS requires are locked, so they always show; an invoice's title
  "Tax Invoice" is set for you when you are GST-registered.
- **Fields**: select **Insert field** under a text to add a field, such as the due date, with a format
  for dates (such as 23 October 2026) or amounts (such as S$1,234.50). A text holds words and fields
  only.

The **live preview** shows the document with your company's own details and a made-up customer. If a
detail is missing from **Settings → Organisation**, such as your address, the preview says which
particular is missing. Select **Save draft** to keep your changes.

## Publish a version

1. Save the draft, then in **Publish** write what changed (optional).
2. Select **Publish**. On invoices and credit notes Fyniq first renders test documents and refuses to
   publish if one lacks a particular IRAS requires.

An Owner or a Controller publishes. In Founder mode they publish alone and the publication joins the
weekly review. Otherwise, and when you prepare templates but do not approve them, select **Ask for
approval**: another Owner or Controller, who did not save the draft, approves it.

## Roll back

In **Versions**, select **Roll back** beside an earlier version. Fyniq publishes it again as the next
version, so the history keeps every step, and your draft stays as it is.

## Choose the default

In **Settings → Templates**, an Owner or a Controller selects **Change default** beside a document type
and chooses one of its published templates, or the Classic starter. Each published template was approved
when it was published, so choosing among them needs no second approval. A customer's or supplier's own
template, chosen with **Templates** next to the contact in **Contacts**, comes before the default.

## What an invoice shows

An invoice renders with the template that applies to its customer, filled from the invoice itself:

- **Before the company is GST-registered** on the invoice's date, the title is "Invoice" and no GST is
  shown.
- **Once registered**, it is a **tax invoice**: the words "Tax Invoice", your company's name, address and
  GST registration number, the invoice's number and date, the customer's name and address, each line's
  description and GST rate, the total before GST, the GST and the total with GST.
- **In another currency**, it also shows the total before GST, the GST and the total with GST in SGD, at
  the invoice's rate. The GST in SGD is the output tax the invoice accounts for.
- **A simplified tax invoice**, when **Simplified tax invoices up to S$1,000** is on in
  **Settings → Organisation** and the total with GST is S$1,000.00 or less, shows the total with GST and
  says "Price payable includes GST." instead of the customer's details and the GST breakdown.
- **Paid** shows what deposits, receipts and credits have settled, and **Amount due** what is left.

A draft invoice whose date is more than 30 days before today shows ⚠: IRAS expects a tax invoice within
30 days of the time of supply. It can still be issued.

## Email an invoice or a quote

- An issued invoice, or a sent quote or sales order, can be emailed to the customer. It goes to the
  customer's billing email, or to the address you give, from your company's invoices sender in
  **Settings → Email**.
- The email's subject and text come from the email template that applies to the customer, and the PDF
  of the document, made with its document template, is attached.
- A draft is never emailed: issue the invoice or send the quote first. A tax invoice that lacks a
  particular IRAS requires, such as the customer's address, is not sent until the detail is added.
- Fyniq keeps the PDF it sent in the document's evidence, and the document lists each time it was
  emailed with its delivery: **Sent**, **Delivered**, **Viewed** or **Bounced**. Emailing it again sends
  the same version again, with a new link.

## The link in the email

- Each email carries a secure link, with **{{document.link}}** in the email template (the starter
  emails have it). Whoever opens it sees the document and its PDF, without signing in, and nothing else.
- The first time it is opened, the document's delivery shows **Viewed**: the time, never who opened it
  or from where.
- On a quote, the customer can **accept** it, giving the name of who accepts, or **decline** it, with a
  comment, while it is still valid and has no answer. The answer is recorded on the quote with its
  date, the name and the comment.
- A link works for 180 days at most. It stops sooner when you **revoke** it from the document's links,
  when the invoice is voided or edited or the quote cancelled, and when the invoice is paid in full.

## Move templates with a bundle

In Expert mode, **Template bundle** exports your templates as one file: each template's version in force,
or its draft if it was never published, with a fingerprint. **Import bundle** brings such a file into
another company as drafts, which publish like any other. An import is refused if a template changed
after it was exported.

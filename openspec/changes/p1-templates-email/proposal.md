# Proposal

## Why

Invoices, quotes, statements, reminders and remittances are issued as PDFs and sent by email. Every
approval, mention and incident needs to reach a person. The render service is a P0 shell, and nothing
sends mail. Wave 2's sales and purchases issue documents and notify approvers, so templates, rendering,
email and notifications land in wave 1 (§8.24, §8.25). Question 44 split the template editor between P1
and P2, and question 45 added the email failover to P1.

## What Changes

- **Templates, as question 44 decided:**
  - the block-based editor with its theme, texts, optional blocks and live preview; drag-and-drop
    layout is P2 (FR-TPL-01, P1 part);
  - fields from a typed catalogue with date and money formats, and no free code; conditional blocks
    and the column chooser are P2 (FR-TPL-02, P1 part);
  - IRAS particulars as locked blocks on tax invoices, simplified tax invoices and credit notes
    (FR-TPL-03);
  - drafts, and immutable numbered versions published under approval, with identical reprints; the
    visual diff is P2 (FR-TPL-04, P1 part);
  - the document types and email templates, with a default per type and a per-customer override
    (FR-TPL-05);
  - 3–4 starter templates built with the design system, and export and import as a versioned bundle
    (FR-TPL-07).
- **Rendering:** the render service compiles Typst templates from document JSON into PDF/A-2a and
  PDF/UA-1, deterministically (ADR-T1).
- **Outbound email (FR-INT-21):**
  - adapters for Zoho ZeptoMail, Resend and SMTP, with Mailpit locally;
  - a sender per purpose, and the sending domain checked daily;
  - provider webhooks for status and suppression;
  - idempotent sends through the outbox, an unknown result reconciled before a retry, and failover
    only for sends that were not accepted (G4-073);
  - a message log without bodies.
- **Inbound email (FR-INT-22):** capture addresses through the Resend inbound adapter. Everything
  received is untrusted, attachments go through the evidence upload path, and unknown senders are
  quarantined.
- **Notifications:**
  - the notification centre (FR-NTF-01);
  - preferences per event type, with channels, digests and quiet hours; security notices cannot be
    turned off (FR-NTF-02);
  - announcement banners, with publishing audited (FR-NTF-03);
  - one event stream feeding every channel (FR-NTF-05).
- **Outside services:** behind interchangeable adapters, with the launch adapters and local
  substitutes of the §8.24 table (2 unnumbered requirements).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `documents/templates`: FR-TPL-01, 02 and 04 (P1 parts), 03, 05 and 07;
- `communication/email-and-notifications`: FR-INT-21, FR-INT-22, FR-NTF-01, 02, 03 and 05, plus the
  2 unnumbered requirements.

## Impact

- **Code:**
  - `cmd/render` and its Typst templates;
  - an `Email` port with ZeptoMail, Resend and SMTP adapters, an `InboundMail` port with Resend and
    Mailpit adapters;
  - migrations for templates, versions, messages, notifications, preferences and banners;
  - the API and the screens.
- **Screens:** announcements, banners, document-templates, notification-centre,
  notification-preferences and template-editor. The template-editor canvas still draws the P2 layout
  features; it is redrawn to the P1 half when this change starts.
- **Needs the founder:**
  - a sending domain with SPF, DKIM and DMARC;
  - ZeptoMail and Resend accounts.

  Until then the adapters run against fakes and Mailpit. Providers that process mail outside
  Singapore need the transfer assessment of FR-INT-21.

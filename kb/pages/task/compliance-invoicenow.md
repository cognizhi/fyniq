---
key: compliance.invoicenow
type: task
title: Send invoices through InvoiceNow and receive suppliers' invoices
summary: Queue a sale, follow it to IRAS's acknowledgement ID, hold the GST return to the filing gate, and turn a supplier's invoice into a draft bill
sources:
  - docs/user-guide/invoicenow.md sha256:14c10222caeb51c8de7f0b7450e5a6b2d5917b65836fbc504e49305fff4263e3
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - Peppol
  - InvoiceNow filing gate
  - IRAS acknowledgement ID
  - file the GST return with a gap
  - invoices from suppliers
---

# Send invoices through InvoiceNow and receive suppliers' invoices

IRAS requires a GST-registered business to submit the invoice data behind its GST return through InvoiceNow, the Peppol network, in phases from 1 April 2026 (new voluntary registrants) to 1 April 2031 (everyone else). The page is **Compliance › InvoiceNow** ([[compliance.gst]] is where the registration is tested).

(src: docs/user-guide/invoicenow.md#what-it-is)

## Steps

1. Pick the return's period. The filing gate lists the documents of the period that have neither IRAS's acknowledgement ID nor a reason to be left out; it reads Ready to file, Waiting for submissions, or Not assessed while the setting "The InvoiceNow requirement applies" is off, and a GST F5 cannot be recorded as filed while it is waiting.
2. Select **Queue for InvoiceNow** beside a sale that was not sent. Fyniq builds the PINT-SG document and checks it against IRAS's published rules first; a document that breaks one is refused with the rule and the field, and never queued.
3. Follow the submission: queued, sent, acknowledged with IRAS's acknowledgement ID, or failed with why. **Send now** and **Check status** do by hand what a run every 15 minutes does; **Queue again** puts a failed one back.

To leave a document out, select **Leave it out** beside it and choose the reason: a reverse charge supply, an import under a permit, a deemed supply, financial services, a supplier not registered for GST, input tax that cannot be claimed, out of scope of GST, or another reason in a few words. It is no longer waited for, and withdrawing the exclusion brings it back.

(src: docs/user-guide/invoicenow.md#the-filing-gate)

## When the network is down

One person with the right to prepare the return asks to file with the gap, with the reason and the evidence of the outage; the request shows on the gate as waiting, and the Owner or the Finance controller selects **Approve the request**, never the person who asked. The unsent documents stay queued and are sent when service returns.

(src: docs/user-guide/invoicenow.md#when-the-network-is-down)

## Invoices from suppliers

**Receive now** reads the invoices suppliers sent. When the sender's Peppol ID or the UEN on the document is a supplier, a draft bill is made, which posts nothing; a second invoice with the same number from the same sender is refused; a document from someone who is not yet a supplier waits until the supplier is added, then select **Make the draft bill**.

(src: docs/user-guide/invoicenow.md#invoices-from-suppliers)

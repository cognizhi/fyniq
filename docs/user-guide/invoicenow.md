---
key: compliance.invoicenow
title: InvoiceNow
mode: expert
roles: [owner, controller, accountant, auditor, tax_agent]
requirements: [FR-AR-12, FR-AP-05, FR-TAX-10, FR-TAX-11]
status: draft
---

# InvoiceNow

## What it is

IRAS requires a GST-registered business to submit the invoice data behind its GST return through InvoiceNow, the Peppol network, in phases from 1 April 2026 (new voluntary
registrants) to 1 April 2031 (everyone else). **Compliance › InvoiceNow** sends your invoices and credit notes, shows each one up to IRAS's acknowledgement ID, receives your
suppliers' invoices as draft bills, and holds the GST return back while a document of its period is still to do.

## The filing gate

Pick the return's period. The gate lists the documents of that period the requirement covers that have neither IRAS's acknowledgement ID nor a reason to be left out.
It reads **Ready to file** when nothing is missing, **Waiting for submissions** when something is, and **Not assessed** while the setting "The InvoiceNow requirement applies"
(Settings › Organisation) is off. A GST F5 cannot be recorded as filed while the gate is waiting.

- Select **Queue for InvoiceNow** beside a sale that was not sent. Fyniq builds its PINT-SG document and checks it against IRAS's published rules first: a document that breaks
  one is refused with the rule and the field to correct, and is never queued.
- A customer on the network gets the document through its Peppol ID. A customer with no Peppol ID, or one the network does not know, goes to IRAS alone.
- An invoice with an exempt line is not sent until the tax agent chooses ES33 or ESN33 in Settings › Organisation.
- A document that needs no submission, such as a reverse charge supply or an import permit, is left out with its reason through the API (`createInvoiceNowExclusion`).

## Submissions

Each submission shows its status: queued, sent, acknowledged (with IRAS's acknowledgement ID) or failed (with why). **Send now** hands a queued one to the access point and
**Check status** reads a sent one; a run every 15 minutes does the same. **Queue again** puts a failed one back. A correction of an invoice is sent as its next version with the
same document UUID.

## When the network is down

If the access point or IRAS was unavailable and the return would be late, one person with the right to prepare the return asks to file with the gap, with the reason and the
evidence of the outage. Someone else with the right to approve (the Owner or the Finance controller) approves it, never the person who asked. The unsent documents stay queued and
are sent when service returns. The business stays responsible for complete submission.

## Invoices from suppliers

**Receive now** reads the invoices suppliers sent through InvoiceNow. Each is kept as received. When the sender's Peppol ID or the UEN on the document is a supplier, a draft bill
is made: its lines go to the supplier's default account and the GST is read as TX at the rate the document states; nothing is posted, and its input-tax facts are left for you to
confirm. A second invoice with the same number from the same sender is refused, naming the first. A document from someone who is not yet a supplier waits: add the supplier,
then select **Make the draft bill**.

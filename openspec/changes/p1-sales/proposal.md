# Proposal

## Why

Fyniq keeps what the company owes its suppliers (`p1-purchases`), but not what its customers owe it. A sale
can only be booked as a manual journal to 1100 Accounts receivable, with nothing tying it to the invoice the
customer received, its due date, its payments, its GST or its reminders, and no ageing of what is open.
Cognizhi invoices a few customers, some monthly and some in USD, and needs a tax invoice that meets the
IRAS particulars once it registers for GST (question 2). Bank import, payment providers, GST returns and
projects all build on invoices (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`).

`p1-sales` waited for `p1-templates-email` (the owner's decision of 22 Oct 2026, `p1-purchases`), which is now
built: templates and their PDFs, outbound email through the providers with the delivery log, and
notifications. `p1-purchases` built the document, approval, posting, payment, allocation and control-account
pipeline that sales mirrors.

## What Changes

- **Invoices (FR-AR-03):**
  - a customer's invoice as a versioned document, with lines (an item, description, quantity, unit price,
    discount, a revenue account, a GST output code) or a single amount, a currency and rate, terms and a due
    date, an optional service period, attachments and notes;
  - five states kept apart: approval, ledger, delivery, settlement and due;
  - posted by the rule `invoice@1`: Dr 1100 the total; Cr revenue the net (`RND-FX`), or 2400 when spread;
    Cr 2200 the output GST (`RND-GST-EXCL`, per GST-code subtotal);
  - approved under FM-2's threshold, voided under FM-1, edited by reversal, as bills are;
  - our number from the invoice sequence when it is issued (posted).
- **Tax invoice particulars (FR-AR-05):** the IRAS particulars on the PDF, SGD amounts on a foreign-currency
  tax invoice, the 30-day check and the simplified tax invoice up to S$1,000.
- **Customer payments recorded by hand (the owner's decision of 24 Oct 2026, until bank import):** a receipt
  into a money account, part or full, against one or more invoices; realised FX per allocation (ENG-41); an
  unapplied amount or overpayment stays a customer credit on 1100. Bank import later matches statement lines
  to these receipts rather than posting them again (FR-BNK-07).
- **Allocations (FR-AR-18):** receipts, credit notes and deposits settle invoices only through allocation
  records, undone by a reversing allocation; no netting with AP without an approved contra.
- **Credit notes and refunds (FR-AR-04):** linked to an invoice in full or part, or standalone; posting the
  reverse of an invoice's legs; refunded from a money account against the credit.
- **Deposits and retainers (FR-AR-09):** a deposits liability until applied; a security deposit without GST;
  a part-payment deposit's GST when received once GST-registered (ENG-45).
- **Write-offs (FR-AR-17):** reason-coded, approval-gated (SOD-9), with evidence; a bad-debt write-off tagged
  for the P2 relief review (FR-AR-11). The settlement tolerances (FR-BNK-08, question 32) post a residual
  after realised FX to 7030 within their caps, for invoices and bills.
- **Quotes and sales orders (FR-AR-01, FR-AR-02):** lines, validity and status, the pipeline value, nothing
  posted; converted to an invoice in full or in part, linked.
- **Sending (FR-AR-06, FR-AR-16):** quotes and invoices by email with the branded PDF (the template the
  document's type and customer choose) and a secure online view link; the customer accepts or declines a
  quote online, kept as evidence. **Tracking (the owner's decision of 24 Oct 2026):** sent, delivered and
  bounced from the provider, and **viewed** when the customer opens the online link; no email open pixels
  (rev 0.103's stance kept).
- **Recurring invoices (FR-AR-07; the owner's decision of 24 Oct 2026):** each set to **make a draft**, the
  default, or **issue and send**; issue-and-send needs its content approved once by a second person, and each
  invoice it issues is posted by the scheduler and listed for the weekly review (FM-3). Month-end dates run
  on the last day of a shorter month.
- **Reminders and statements (FR-AR-08; the owner's decision of 24 Oct 2026):** a schedule per customer
  group (by default 3 days before due, on the due date, 7 and 30 days overdue) with templates; **until bank
  import reconciles receipts, each due reminder waits in the Inbox** to be sent in one click or skipped; an
  Owner switches a group to automatic once receipts come from the bank. A statement of account by date range
  with the brought-forward balance, documents, payments, FX and an ageing that adds up.
- **AR ageing (FR-AR-10):** Current, 1–30, 31–60, 61–90 and 90+ by customer and currency, with the unrealised
  FX exposure; the open documents and credits add up to 1100 (INV-07).
- **The screens** `invoice-editor`, `invoice-record`, `quotes-list`, `record-payment`, `recurring-documents`,
  `statements-reminders` and `customer-document-view`, as designed in `docs/design/screens/`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `sales/receivables`: FR-AR-01, FR-AR-02, FR-AR-03, FR-AR-04, FR-AR-05, FR-AR-06, FR-AR-07, FR-AR-08 (its P1
  part), FR-AR-09, FR-AR-10, FR-AR-16, FR-AR-17 and FR-AR-18;
- `ledger/posting-rules`: "Posting an invoice", "Amounts of a posted invoice", the customer half of "Customer
  and supplier payments", "Customer refunds and deposits", "Credit documents and opening items" (credit notes),
  the invoice half of "Spreading an invoice or bill", "Settlement difference on an invoice or bill",
  "Settlement tolerances" and "Bad-debt write-off" (its posting; the relief tracking is FR-AR-11, P2).

The owner's decisions of 24 Oct 2026 change the wording of FR-AR-03, FR-AR-06 and FR-AR-16 ("opened" becomes
"viewed through its online link") and add to FR-AR-07, FR-AR-08 and FR-AR-18; PRD-001 rev 0.115 records them
and the main specs follow in the same pull request.

## Before it starts

Built and needed: documents and versions; the posting and balance services; periods and the close; the
approval engine with FM-2's threshold; contacts (customers) and items; number sequences; evidence; the rate
cascade and realised FX (`p1-multi-currency-fx`); the scheduler; payments, allocations and the AP sub-ledger
(`p1-purchases`), which this change generalises; templates, their PDFs, outbound email, the delivery log and
notifications (`p1-templates-email`).

What it cannot finish alone stays open here, named in `tasks.md`:
- matching receipts from bank statements: `p1-bank-import-reconcile` (FR-BNK-07), which also turns reminders
  automatic per customer group;
- InvoiceNow (FR-AR-12, P2, or earlier if GST registration is planned), online payment (FR-AR-13, P2), the
  customer portal (FR-AR-14, P2), subscription revenue schedules (FR-AR-15, P2), disputes and promise-to-pay
  (FR-AR-08's P2 part) and bad-debt relief tracking (FR-AR-11, P2);
- the invoice, receipt, credit and deposit rules in the independent oracle: an oracle session (QA-ENG-01), not
  a build session;
- FX revaluation of invoices one by one: with this change's open invoices (`p1-multi-currency-fx` task 3.6).

Question 32 (settlement tolerances) is the accountant's: the settings ship at the workbook's values, so
tolerances apply provisionally until the accountant confirms them.

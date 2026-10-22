# Proposal

## Why

Fyniq posts manual journals, revalues foreign-currency balances and closes months, but it keeps no record
of what the company owes its suppliers. A supplier's bill can only be booked as a manual journal to 2010
Accounts payable. Nothing links that journal to the bill, its due date, its payments or its GST, and no
ageing shows what is open. Cognizhi's common purchases are monthly SaaS subscriptions, many in USD,
annual subscriptions paid up front, and the occasional non-resident consultant. Each needs the bill
document, input tax as the supplier states it, a spread over the months it covers, or a withholding-tax
decision. Expense claims, fixed assets, founders' money and bank import all build on bills
(`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`).

The plan lists `p1-sales` before `p1-purchases` in wave 2. **On 22 Oct 2026 the owner chose to build
purchases first**, for three reasons:
- every change in its "After" column is built far enough: `p1-general-ledger-close` and
  `p1-documents-evidence`;
- `p1-sales` needs `p1-templates-email`, which has not started, for its PDFs, sending, reminders and
  statements;
- it is the smaller change (10 requirements and 4 screens against 22 and 7), and the document, approval,
  posting and control-account pipeline it builds is the one sales then mirrors.

## What Changes

- **Bills (FR-AP-02):**
  - a supplier's bill as a versioned document, with lines (an expense or asset account, a GST input
    code and dimensions), the supplier's own number and ours, dates, terms, currency and rate,
    attachments (FR-DOC-05), and an optional prepayment period per line;
  - separate approval, ledger, payment and due states, as for invoices (FR-AR-03);
  - posted by the rule `bill@1`, as the posting rules "Posting a bill" and "Amounts of a posted bill"
    state:
    - Dr the expense or asset with the net (`RND-FX`), or 1210 when spread;
    - Dr 2200 with the input tax as the supplier's tax invoice states it in SGD (ENG-42);
    - Cr 2010 with the total;
    - ⚠ when the stated GST differs from `RND-GST-EXCL` by more than rounding;
  - approved under FM-2's threshold per document, and voided under FM-1, as journals are.
- **Payments recorded by hand** (the owner's decision of 22 Oct 2026, until bank import exists):
  - a payment from a money account, part or full, posts Dr 2010 and Cr the money account, with realised
    FX per payment (ENG-41);
  - bank import later matches statement lines to these payments rather than posting them again
    (FR-BNK-07).
- **Vendor credits and refunds (FR-AP-03):** a credit posts the reverse of a bill's legs. It is applied
  to that supplier's bills, or settled by a refund into a money account.
- **AP ageing, bills to pay and the cash requirement (FR-AP-06)**, from the open documents, which add up
  to 2010 (INV-07).
- **Track and pay (FR-AP-13):**
  - the bills board by status, and a due-date calendar;
  - a bill scheduled for a payment date;
  - reminders before due dates, in the Inbox;
  - early-payment discounts shown.
- **Prepayment spreads (FR-GL-03, ENG-05;** the owner's decision of 22 Oct 2026**):**
  - a spread line posts to 1210;
  - each month's release (`ALLOC-EVEN`) is posted by the scheduler, up to the current month;
  - a void or an edit reverses the releases with the bill.
- **Recurring bills (FR-AP-09;** the owner's decision of 22 Oct 2026**):**
  - the scheduler makes each occurrence's draft through one database function, as for recurring
    journals;
  - a person checks and posts it, so nothing recurring posts by itself.
- **Purchase orders (FR-AP-01):** lines, approval (FR-APR-01), and conversion to a bill. The three-way
  match is P2.
- **Non-resident payments (FR-AP-19):**
  - the supplier's tax residence;
  - the nature of the payment;
  - the tax agent's withholding decision, kept as evidence and reused for that supplier and nature;
  - ⚠ on the bill until a decision exists;
  - a withholding posting: the gross expense, the net to the supplier, the tax to a withholding-tax
    payable account.
- **Input-tax eligibility (FR-AP-20):** decided per line from recorded facts, never from an extracted
  amount, with the condition that decided it. Cognizhi is not GST-registered (question 2), so every
  line is "not claimable: the organisation is not GST-registered" until it registers.
- **The screens** `bill-editor`, `bill-record`, `bills-board` and `purchase-orders`, as designed in
  `docs/design/screens/`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `purchases/payables`: FR-AP-01 (the PO), FR-AP-02, FR-AP-03, FR-AP-06, FR-AP-09, FR-AP-13 (P1 parts),
  FR-AP-19 and FR-AP-20;
- `ledger/posting-rules`: "Posting a bill" and "Amounts of a posted bill", and, by the owner's decision,
  the supplier half of "Customer and supplier payments" and the bill half of "Spreading an invoice or
  bill".

A decision that changes a requirement's text (see `design.md`) goes through a PRD revision and the main
specs in the pull request that builds it, as `p1-multi-currency-fx` did.

## Before it starts

`p1-general-ledger-close` and `p1-documents-evidence` are built except for tasks that wait on other
changes. This change needs only what is built:
- documents and versions;
- the posting service and the balance service;
- periods and the close;
- the approval engine and FM-2's threshold;
- contacts (vendors) and number sequences;
- evidence attachments;
- the rate cascade, realised FX and the foreign-currency input tax (`p1-multi-currency-fx`);
- the scheduler's platform jobs.

What it cannot finish alone stays open here, named in `tasks.md`:
- remittance advice and reminder emails: `p1-templates-email`. Until then reminders go to the Inbox;
- payment batches and the "in a batch" state: `p1-bank-import-reconcile` (FR-BNK-12);
- the withholding tax's statutory-calendar task: the calendar of `p1-gst`;
- capture and automated bill entry (FR-AP-04, FR-AP-12): `p1-document-capture`;
- the bill rules in the independent oracle: an oracle session (QA-ENG-01), not a build session;
- FX revaluation of bills one by one (`p1-multi-currency-fx` task 3.6): with this change's open bills.

Question 32 (settlement tolerances) decides what happens to a residual after realised FX. Until it is
answered, a residual stays open on the bill or is written off with a reason (FR-BNK-08).

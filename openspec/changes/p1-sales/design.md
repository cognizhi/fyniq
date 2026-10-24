# Design

## Context

- **The purchases pipeline is the pattern** (`p1-purchases`, built): a versioned document (`bill`) with an
  approval, ledger, payment and due state; the posting rule `bill@1` with its replay golden; FM-2's threshold,
  FM-1 for a void and SOD-3 through the approval engine; edits by reversal (FR-GL-12); payments recorded by hand
  (`bill_payment`) with allocations that never change and are undone by a reversing allocation (FY170, FY171);
  credits applied and refunded; the AP sub-ledger reconciled to 2010 at the close (INV-07); recurring bills made as
  drafts by the scheduler through one database function; bill reminders in the Inbox (`payables.Reminders`).
- **Contacts and items:** customers have terms, a default revenue account, a GST code, a currency, billing and
  remittance emails, and their own templates (`p1-templates-email`); items have a price, a revenue account and a
  GST code (FR-CT-02).
- **The chart:** 1100 Accounts receivable (role `accounts-receivable`), 2200 GST control, 2400 Deferred revenue,
  2410 Customer deposits & retainers (no role yet), 4010 to 4095 revenue, 7030 FX & rounding. **No bad-debt
  account:** Appendix B.5 leaves its code to the CoA review.
- **Templates and email (`p1-templates-email`):** document templates per type with a default and a customer's
  own; the render service makes the PDF; outbound email through the outbox with the delivery log and the
  providers' events (delivered, bounced, complained; never opens or clicks, rev 0.103); notifications and the
  Inbox.
- **Appendix D, "Invoices, credit notes":** Accountant and AR specialist CP (create and post); Bookkeeper C;
  Controller A; Approver A up to a limit; Owner, Auditor and Tax agent V.
- **GST:** Cognizhi is not GST-registered (question 2). An invoice carries output codes from `sg-gst` (SR, ZR,
  ES, OS…); until registration they post no GST and the PDF is not a tax invoice.

## Goals / Non-Goals

**Goals:**
- An invoice is drafted, approved, issued, sent, paid, credited, written off, voided and edited with the ledger
  right at every step, and AR's open documents and credits add up to 1100.
- What a customer receives is the document as posted, with the IRAS particulars once registered, through a link
  only they hold.
- Nothing reaches a customer without a person, unless a second person approved a recurring invoice's content
  for issue-and-send.

**Non-Goals:**
- InvoiceNow (FR-AR-12), online payment (FR-AR-13), the customer portal (FR-AR-14), subscription revenue
  schedules (FR-AR-15), disputes and promise-to-pay, bad-debt relief tracking (FR-AR-11): P2.
- Receipts from bank statements and automatic reminders: `p1-bank-import-reconcile`.
- Dimensions on invoice lines: with FR-DIM.

## Decisions

The owner decided four of these on 24 Oct 2026, interactively (PRD-001 rev 0.115); the rest are provisional, on
the recommended option, until the accountant's sign-off. A decision that changes a requirement's text goes into
a PRD revision with the slice that builds it.

- **Document types:** `invoice`, `credit_note`, `customer_receipt`, `customer_refund`, `customer_deposit`,
  `write_off`, `quote` and `sales_order` (the last two post nothing), each with versions; recurring invoices, a
  template with versions as recurring bills have. The bill types generalise where they can: allocations and
  their guards become the documents' own, not the bills' alone.
- **An invoice's content**, one payload per version: customer; date (the tax point, ENG-45); due date from the
  customer's terms or FR-ORG-04's 30 days; currency and rate (typed or from the cascade); lines (item,
  description, quantity, unit price, discount %, revenue account, GST code), or one amount; an optional service
  period (first month and number of months); the template; notes; the quote or order it came from.
- **Our number:** from the invoice sequence (`INV-`, gap-free, FR-ORG-04), given when the invoice is issued
  (posted); a draft has none. Credit notes `CN-`, receipts `RC-`, refunds `CR-`, deposits `DP-`, quotes `QT-`,
  orders `SO-`.
- **States, kept apart (FR-AR-03):** approval (draft, awaiting approval, approved, rejected); ledger (not
  posted, posted, voided by reversal); delivery (not sent, sent, delivered, **viewed**, bounced); settlement
  (open, part-paid, paid, written off), from its allocations; due (not due, overdue), from the due date and the
  open amount. Only posting and voiding change the ledger.
- **Approval as for bills:** above FM-2's threshold in SGD an invoice issues only on another person's approval;
  at or below it a Founder-mode issue is logged as self-approved for the weekly review (FM-3); whoever prepared
  the version never approves it (SOD-3); a void needs a second person in Founder mode (FM-1).
- **The posting rule `invoice@1`:** Dr 1100 the total; Cr each line's revenue account with its net at the
  invoice's rate under `RND-FX`, or 2400 when the line is spread; Cr 2200 the output GST under `RND-GST-EXCL`
  (per GST-code subtotal, or per line by the setting), only once the organisation is GST-registered on the
  invoice's date; a foreign invoice's GST in SGD at the invoice's rate (ENG-42). A replay golden.
- **Spreads:** a spread line posts its net to 2400 and the scheduler releases it monthly from the service start
  month under `ALLOC-EVEN`, as bills' spreads release from 1210; a void or edit reverses the releases with it.
- **The AR sub-ledger:** the open amounts of posted invoices, credit notes, unapplied receipts and deposits
  applied, by customer, equal to 1100 (INV-07), compared at the close as AP is.
- **Receipts by hand (the owner, 24 Oct 2026):** a `customer_receipt` names a money account, a date, an amount
  and how it is applied to the customer's invoices; it posts Dr the money account at the receipt's rate and Cr
  1100 each invoice's applied amount at its stored rate, realised FX to 7030 (`ledger.RealisedFX`); an unapplied
  amount or overpayment stays a customer credit on 1100, shown as unapplied; a void reverses it and reopens the
  invoices. Two receipts allocated at once to one invoice: the database lets one settle it and the other stay a
  credit (G4-054).
- **Settlement tolerances (FR-BNK-08, question 32):** after realised FX, a residual within the organisation's
  tolerance (the settings `bank.tolerance_*`, at the workbook's values) posts to 7030 in the month of the last
  payment, within the caps per document and per month; a larger one stays open or is written off. They apply to
  bills too, which have waited for them. Provisional until the accountant confirms question 32.
- **Write-offs (FR-AR-17):** a `write_off` with one reason (discount, charges, bad debt, cross-currency
  difference, migrated), evidence, and approval as a write-off by someone other than whoever recorded the
  receipt (SOD-9); it posts Dr the reason's account and Cr 1100. Discount goes to 4095, charges and
  cross-currency differences to 7030; **a bad debt's account is chosen when it is recorded** until the CoA
  review assigns one (Appendix B.5) `[verify]` with the accountant; a bad debt is tagged for the P2 relief review.
- **Credit notes (FR-AR-04):** the reverse of an invoice's legs, linked to the invoice in full or part, or
  standalone; applied to the customer's invoices by allocation (posting nothing in one currency, realised FX
  across rates), or refunded from a money account (`customer_refund`: Dr 1100, Cr the money account).
- **Deposits and retainers (FR-AR-09):** a `customer_deposit` posts Dr the money account, Cr 2410, which gains the
  role `customer-deposits`; applying it to an invoice moves it (Dr 2410, Cr 1100). A security deposit is a
  liability without GST until applied. A part-payment deposit received once GST-registered posts its output GST
  when received (ENG-45) and the final invoice deducts it. 2410's classification is the CoA review's
  `[verify]`.
- **Quotes and sales orders (FR-AR-01, FR-AR-02):** the same lines as an invoice, a validity date, and the
  statuses Draft, Sent, Accepted, Declined, Invoiced, Cancelled, Expired; the pipeline value is the open quotes'
  and orders' totals in SGD; nothing posts. Conversion makes a draft invoice from all lines or the ones chosen,
  each linked both ways; a quote past its validity date is Expired by the scheduler.
- **The online link (FR-AR-06, FR-AR-16):** sending a quote or an invoice makes a link with a random 256-bit
  token, kept only as its SHA-256, for that document version; it shows the document and its PDF and nothing
  else, through a public read endpoint with no session, rate-limited, `Cache-Control: no-store`, never indexed.
  It lasts until the document is settled, voided or 180 days pass, and a person can revoke it. A quote's link
  also accepts or declines it, with the customer's name and comment kept as evidence (FR-DOC-05). Provisional;
  its threat model comes with the slice.
- **Tracking (the owner, 24 Oct 2026):** delivery is sent, delivered and bounced from the provider's events, and
  **viewed** the first time the online link is opened (the time, never the reader's address); no open pixels or
  click tracking. *Not chosen:* the providers' open tracking, and delivery only.
- **Sending:** to the customer's billing email (or one typed for this send), from the organisation's invoices
  sender (FR-INT-21), the subject and body from the email template the customer or type chooses, the PDF of the
  issued version attached, and the link. A draft is never sent; a reminder or a resend sends the same version.
- **Recurring invoices (the owner, 24 Oct 2026):** each is **make a draft** (the default) or **issue and send**;
  the scheduler makes each occurrence through one database function, as for recurring bills; issue-and-send is
  allowed only once a second person approved the recurring invoice's content version (FR-APR-01), and each
  invoice it issues is posted by the scheduler's principal and listed for the weekly review. A change to the
  content needs the approval again. Weekly to yearly; the 29th to 31st run on a shorter month's last day.
  *Not chosen:* drafts only, and issue-and-send by default.
- **Reminders (the owner, 24 Oct 2026):** a schedule per customer group (default: 3 days before due, on the due
  date, 7 and 30 days overdue), with the email templates; **until bank import reconciles receipts, a due
  reminder waits in the Inbox** for a person to send in one click or skip; an Owner switches a group to automatic
  once receipts come from bank import (`p1-bank-import-reconcile`). A quote about to expire is reminded the same
  way. *Not chosen:* automatic from day one, and off by default.
- **Statements (FR-AR-08):** by customer and date range: the brought-forward balance, the documents, receipts,
  credits, write-offs, FX and rounding, and an ageing that adds to the closing balance (the workbook's Statement
  logic); as a PDF from its template, sent like an invoice.
- **AR ageing (FR-AR-10):** Current, 1–30, 31–60, 61–90 and 90+ by customer and currency, unapplied credits shown
  apart, and the unrealised FX exposure at the latest rate.
- **Tax invoice particulars (FR-AR-05):** the invoice template carries the IRAS particulars (the supplier's name,
  address and GST number, the number and date, the customer, the description, the amount before GST, the rate,
  the GST, the total), the SGD amounts on a foreign-currency tax invoice, and the words "Tax invoice" only once
  GST-registered on its date; ⚠ when an invoice is issued more than 30 days after its time of supply; a
  simplified tax invoice up to S$1,000. The templates' particulars check (`p1-templates-email`) already refuses
  a template that drops one.

## Risks / Trade-offs

- **Receipts by hand drift from the bank.** Until bank import, a receipt not recorded leaves an invoice overdue;
  the reminders' Inbox step is the guard, and bank import later matches rather than reposts.
- **A public link is an attack surface.** Unguessable tokens kept hashed, one version, no listing, rate limits, an
  expiry and revocation; the threat model with the sending slice.
- **Issue-and-send posts revenue with no person at the time.** It needs a second person's approval of the content,
  lists each invoice for the weekly review, and a change of content needs the approval again.
- **Generalising the bills' allocations** touches built tables. Expand and contract: new columns and functions
  beside the bills', the bills' tests unchanged.
- **The accountant's open points:** question 32's tolerances, a bad-debt account (B.5), and 2410's classification,
  each provisional and marked `[verify]`.

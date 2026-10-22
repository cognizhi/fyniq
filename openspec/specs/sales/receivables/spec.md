# sales/receivables Specification

## Purpose
How Fyniq handles what customers owe: quotes and orders, invoices and their states, credit notes, deposits, allocations of receipts and credits, reminders and statements, ageing, write-offs, InvoiceNow, online payment, the customer portal and subscription revenue schedules. Derived from PRD-001 §8.5.

## Requirements

### Requirement: FR-AR-01 Quotes and sales orders
The system SHALL keep quotes and sales orders with lines, a validity date and a status (Draft, Sent, Accepted, Declined, Invoiced, Cancelled, Expired), and SHALL show their pipeline value.

Source: PRD-001 §8.5 · Priority S · Phase P1

#### Scenario: Lines and validity date
- **WHEN** a quote or sales order is saved with lines and a validity date
- **THEN** the lines and the validity date are stored on it

#### Scenario: Statuses
- **WHEN** a quote or sales order moves through its life
- **THEN** its status is one of Draft, Sent, Accepted, Declined, Invoiced, Cancelled or Expired

#### Scenario: Pipeline value
- **GIVEN** quotes and sales orders exist
- **WHEN** a user views them
- **THEN** their pipeline value is shown

#### Scenario: Quotes and orders are not posted
- **WHEN** a quote or sales order is created, sent, accepted or cancelled
- **THEN** nothing is posted to the ledger

### Requirement: FR-AR-02 Convert a quote or order to an invoice
The system SHALL convert a quote or sales order into an invoice, in full or in part, keeping the links between them.

Source: PRD-001 §8.5 · Priority S · Phase P1

#### Scenario: Full conversion
- **WHEN** a user converts a quote or order in full
- **THEN** an invoice is created with all of its lines

#### Scenario: Partial conversion
- **WHEN** a user converts part of a quote or order
- **THEN** an invoice is created for that part only

#### Scenario: Links are kept
- **WHEN** an invoice is created from a quote or order
- **THEN** the invoice and the quote or order stay linked to each other

### Requirement: FR-AR-03 Invoices
The system SHALL keep invoices with a number, date, customer, lines or a single amount, currency and rate, terms and due date, service period, attachments and notes, and SHALL track each invoice in five separate states: approval, ledger, delivery, settlement and due.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Invoice lines
- **WHEN** an invoice is entered with lines
- **THEN** each line has an item, description, quantity, unit price, discount %, account, GST code and dimensions

#### Scenario: Single-amount invoice
- **WHEN** an invoice is entered as a single amount instead of lines
- **THEN** it is accepted

#### Scenario: Service period for deferred revenue
- **WHEN** an invoice carries a service period
- **THEN** the service period is used for its deferred revenue

#### Scenario: Approval state
- **WHEN** an invoice is in the approval process
- **THEN** its approval state is draft, awaiting approval, approved or rejected

#### Scenario: Ledger state
- **WHEN** an invoice's ledger state is shown
- **THEN** it is not posted, posted, or voided by reversal

#### Scenario: Delivery state
- **WHEN** an invoice's delivery state is shown
- **THEN** it is not sent, sent, opened or bounced, with its Peppol transmission status where sent by InvoiceNow (FR-AR-12)

#### Scenario: Settlement state is derived from allocations
- **WHEN** receipts, credits or write-offs are allocated to an invoice
- **THEN** its settlement state (open, part-paid, paid, written off) is derived from those allocations (FR-AR-18)

#### Scenario: Due state is derived
- **WHEN** an invoice's due state is shown
- **THEN** it is not due or overdue, derived from the due date and the open balance

#### Scenario: Only posting and voiding change the ledger
- **WHEN** an invoice is posted or voided
- **THEN** the ledger changes
- **AND** a delivery failure or the invoice becoming overdue never changes the ledger

#### Scenario: G4-045 Overpayment, credit applied, allocation reversed, bounce and overdue
- **GIVEN** an invoice of S$1,000.00 paid S$1,050.00, whose S$50.00 credit is later applied to the next invoice
- **WHEN** one allocation is then reversed, a sent email bounces and the invoice becomes overdue
- **THEN** INV-07 holds at every month end (1100 = open documents + unapplied credits)
- **AND** ageing shows the S$50.00 as unapplied until applied
- **AND** the reversed allocation restores the open balance
- **AND** the bounce and the overdue state post nothing

### Requirement: FR-AR-04 Credit notes and refunds
The system SHALL support credit notes linked to invoices, in full or in part, standalone credit notes, and refunds.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Credit note linked to an invoice
- **WHEN** a credit note is raised against an invoice for all or part of it
- **THEN** the credit note is linked to that invoice

#### Scenario: Standalone credit note
- **WHEN** a credit note is raised without an invoice
- **THEN** it is accepted as a standalone credit note

#### Scenario: Refund
- **WHEN** a credit note is refunded to the customer
- **THEN** the refund is recorded against that credit note

### Requirement: FR-AR-05 Tax invoice particulars
The system SHALL lay out tax invoices to meet the IRAS particulars.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Required particulars
- **WHEN** a tax invoice is produced
- **THEN** it shows the supplier name, address and GST number, the invoice number and date, the customer, the description, the amount before GST, the rate, the GST and the total

#### Scenario: Foreign-currency tax invoice
- **WHEN** a tax invoice is in a foreign currency
- **THEN** it also shows the net, GST and gross amounts in SGD

#### Scenario: Issued within 30 days
- **WHEN** a tax invoice is required
- **THEN** it is issued within 30 days of the time of supply

#### Scenario: Simplified tax invoice
- **WHEN** the invoice total is up to S$1,000
- **THEN** a simplified tax invoice may be issued

### Requirement: FR-AR-06 Branded PDF templates, email and online view
The system SHALL produce invoices as branded PDFs from templates made in the template editor (§8.25), several per organisation, send them by email from the platform with tracking, and give each a secure online view link.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Several templates per organisation
- **WHEN** an organisation creates templates in the editor (§8.25)
- **THEN** it can keep several branded PDF templates and choose one per document

#### Scenario: Email with tracking
- **WHEN** an invoice is sent by email from the platform
- **THEN** its sent and opened events are tracked

#### Scenario: Secure online view link
- **WHEN** an invoice is sent
- **THEN** it carries a secure link to view it online

### Requirement: FR-AR-07 Recurring invoices
The system SHALL create recurring invoices at frequencies from weekly to yearly, as drafts or sent automatically, with month-end date handling.

Source: PRD-001 §8.5 · Priority S · Phase P1

#### Scenario: Frequency
- **WHEN** a recurring invoice is set up
- **THEN** its frequency can be anything from weekly to yearly

#### Scenario: Auto-draft
- **GIVEN** a recurring invoice set to auto-draft
- **WHEN** its next date arrives
- **THEN** a draft invoice is created

#### Scenario: Auto-send
- **GIVEN** a recurring invoice set to auto-send
- **WHEN** its next date arrives
- **THEN** the invoice is created and sent

#### Scenario: Month-end dates
- **GIVEN** a recurring invoice scheduled on the 29th, 30th or 31st
- **WHEN** a later invoice falls in a shorter month
- **THEN** it runs on the last day of that month

### Requirement: FR-AR-08 Automated reminders and statements
The system SHALL send automated reminders on a schedule per customer group, with templates, and SHALL produce a statement of account by date range using the workbook Statement logic.

Source: PRD-001 §8.5 · Priority S · Phase P1 / P2 (disputes and promise-to-pay)

#### Scenario: Reminder schedule per customer group
- **GIVEN** a reminder schedule and templates for a customer group
- **WHEN** an invoice of a customer in that group reaches a scheduled point
- **THEN** a reminder from the group's templates is sent

#### Scenario: Statement of account
- **WHEN** a statement of account is produced for a date range
- **THEN** it shows the brought-forward balance, the documents, the payments, FX and rounding, and an ageing that adds to the balance

#### Scenario: Disputed invoice pauses reminders (P2)
- **WHEN** an invoice is marked disputed with an amount and a reason
- **THEN** its reminders pause until the dispute is resolved

#### Scenario: Promise-to-pay date pauses reminders (P2)
- **WHEN** an invoice carries a promise-to-pay date
- **THEN** its reminders pause until that date passes

#### Scenario: Ageing separates disputed amounts (P2)
- **GIVEN** disputed invoices
- **WHEN** AR ageing (FR-AR-10) is shown
- **THEN** disputed and undisputed amounts are shown separately

### Requirement: FR-AR-09 Customer deposits and retainers
The system SHALL record customer deposits and retainers and apply them to invoices.

Source: PRD-001 §8.5 · Priority S · Phase P1

#### Scenario: Deposit applied to an invoice
- **WHEN** a deposit or retainer is applied to an invoice
- **THEN** the invoice's open balance is reduced by the amount applied

#### Scenario: Part-payment deposit when GST-registered
- **GIVEN** the organisation is GST-registered
- **WHEN** a deposit that is a part payment is received
- **THEN** output GST is posted when it is received (ENG-45)
- **AND** the final invoice deducts the deposit

#### Scenario: Security deposit
- **WHEN** a security deposit is received
- **THEN** it is a liability without GST until it is applied

### Requirement: FR-AR-18 Allocations and credits
The system SHALL apply receipts, credit notes and deposits to documents by allocation records, each with its source, document, amount in the document currency and in base, date, and realised FX per allocation (ENG-41).

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Undoing an allocation
- **WHEN** an allocation is undone
- **THEN** a reversing allocation is recorded
- **AND** the original allocation is never edited

#### Scenario: Unapplied receipt or overpayment
- **WHEN** a receipt is unapplied or overpays
- **THEN** the excess stays on 1100 as a customer credit under that customer
- **AND** it is shown as unapplied in ageing and statements

#### Scenario: Using a customer credit
- **GIVEN** a customer credit
- **WHEN** it is used
- **THEN** it is applied later, refunded (Dr 1100, Cr Bank), or written off only by an approved write-off (FR-AR-17)

#### Scenario: Deposits liability
- **WHEN** a customer deposit or retainer (FR-AR-09) is received
- **THEN** it posts to a deposits liability until applied [account and classification: CoA review, accountant to confirm]

#### Scenario: Cross-currency allocation
- **WHEN** a receipt in one currency is applied to a document in another
- **THEN** the allocation records the document-currency amount settled and the rate

#### Scenario: No netting without a contra
- **GIVEN** a contact that is both a customer and a supplier
- **WHEN** its balances would be offset
- **THEN** they are never netted without an approved contra (FR-APR-01)

#### Scenario: Supplier credits mirror this
- **WHEN** a supplier credit or refund arises
- **THEN** AP handles it by the same allocation rules (FR-AP-03)

#### Scenario: Built for bills (rev 0.77)
- **WHEN** a payment of bills settles a bill
- **THEN** its allocation records the payment and its version, the bill, the date, the amount in the bill's currency, the carrying amount cleared in the base currency and the realised FX
- **AND** it never changes or goes, and a reversing allocation negates it exactly, once, on or after its date (FY170, FY171)

#### Scenario: Supplier credits applied later, built (rev 0.81)
- **WHEN** a supplier credit is applied to a bill
- **THEN** one allocation settles the bill and one the credit, each with the carrying amount it cleared

#### Scenario: G4-054 Two receipts allocated at once to one invoice
- **WHEN** two S$1,000 receipts are allocated at the same time to one S$1,000 invoice
- **THEN** one settles the invoice
- **AND** the other becomes a customer credit or stays unapplied
- **AND** AR is never negative and INV-07 holds

### Requirement: FR-AR-10 AR ageing
The system SHALL report AR ageing in the buckets Current, 1–30, 31–60, 61–90 and 90+ by customer and currency, with the unrealised FX exposure.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Ageing buckets by customer and currency
- **WHEN** AR ageing is produced
- **THEN** open amounts are shown in Current, 1–30, 31–60, 61–90 and 90+ by customer and by currency

#### Scenario: Unrealised FX exposure
- **GIVEN** open receivables in a foreign currency
- **WHEN** AR ageing is produced
- **THEN** the unrealised FX exposure on them is shown

### Requirement: FR-AR-11 Bad-debt write-off with GST relief tracking
The system SHALL support an approval-gated bad-debt write-off (SOD-9) with tracking of GST bad-debt relief.

Source: PRD-001 §8.5 · Priority S · Phase P2

#### Scenario: Write-off is approval-gated
- **WHEN** a bad debt is written off
- **THEN** the write-off needs approval under SOD-9

#### Scenario: Bad-debt relief conditions are tracked
- **GIVEN** a written-off bad debt
- **WHEN** its GST bad-debt relief is tracked
- **THEN** it shows whether the debt is written off, whether 12 months have passed since the date of the supply or the debtor is insolvent, and whether the claim is within 5 years of the supply

### Requirement: FR-AR-17 Write-off of a receivable or payable residual
The system SHALL write off a receivable or payable residual only by a reason-coded, approval-gated write-off with evidence, posted to the account the reason names.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Reason codes
- **WHEN** a write-off is recorded
- **THEN** it carries one reason code: discount, charges, bad debt, cross-currency difference or migrated
- **AND** it posts to the account that reason names

#### Scenario: Approval and segregation
- **WHEN** a write-off is submitted
- **THEN** it needs approval as a write-off (FR-APR-01)
- **AND** it is never approved by the person who recorded the receipt (SOD-9)

#### Scenario: Evidence
- **WHEN** a write-off is submitted
- **THEN** it carries its evidence

#### Scenario: Bad debt tagged for later relief review
- **GIVEN** the GST bad-debt-relief tracking of FR-AR-11 (P2) is not yet available
- **WHEN** a bad-debt write-off is recorded
- **THEN** it is tagged for later relief review

### Requirement: FR-AR-12 InvoiceNow (Peppol)
The system SHALL send invoices, credit notes and debit notes by InvoiceNow (Peppol) in PINT-SG format through an IMDA-accredited Access Point, and MUST be live before Cognizhi registers voluntarily for GST.

Source: PRD-001 §8.5 · Priority M before GST registration; else S · Phase P2 (or earlier if GST registration is planned)

#### Scenario: Send through an Access Point
- **WHEN** an invoice, credit note or debit note is sent by InvoiceNow
- **THEN** it goes in PINT-SG format through an IMDA-accredited Access Point

#### Scenario: Transmission status per document
- **WHEN** a document has been sent by InvoiceNow
- **THEN** its transmission status is tracked up to the IRAS acknowledgement ID (FR-TAX-10)

#### Scenario: Customer Peppol ID is validated
- **WHEN** a document is to be sent to a customer by InvoiceNow
- **THEN** the customer's Peppol ID is validated

#### Scenario: Live before voluntary GST registration
- **WHEN** Cognizhi registers voluntarily for GST
- **THEN** InvoiceNow is already live, because it is mandatory for new voluntary registrants since 1 Apr 2026, and all registrants are phased in 2028–2031 (§14.2)

### Requirement: FR-AR-13 Online payment
The system SHALL offer online payment of invoices: a PayNow QR (SGQR) on invoices, and card and bank payment links through a licensed provider (e.g. Stripe).

Source: PRD-001 §8.5 · Priority S · Phase P2

#### Scenario: PayNow QR on an invoice
- **WHEN** an invoice is produced
- **THEN** it can carry a PayNow QR (SGQR)

#### Scenario: Card and bank payment links
- **WHEN** an invoice is sent
- **THEN** it can carry a card or bank payment link from a licensed provider (e.g. Stripe)

#### Scenario: Payouts and fees matched automatically
- **WHEN** the provider pays out the collected payments
- **THEN** the payouts and fees are matched automatically

### Requirement: FR-AR-14 Customer portal
The system SHALL offer a customer portal where a customer views and pays their invoices and downloads statements.

Source: PRD-001 §8.5 · Priority C · Phase P2

#### Scenario: View and pay invoices
- **WHEN** a customer signs in to the portal
- **THEN** they can view their invoices and pay them

#### Scenario: Download statements
- **WHEN** a customer is in the portal
- **THEN** they can download their statements

### Requirement: FR-AR-15 Subscription revenue schedules
The system SHALL keep subscription revenue schedules (start, end, MRR) that feed deferred revenue and KPIs, and SHALL record contract changes (upgrade, downgrade, cancel).

Source: PRD-001 §8.5 · Priority S · Phase P2

#### Scenario: Schedule feeds deferred revenue and KPIs
- **WHEN** a subscription schedule with a start, an end and an MRR is saved
- **THEN** it feeds deferred revenue and the KPIs

#### Scenario: Change is a dated amendment
- **WHEN** a contract is upgraded, downgraded or cancelled
- **THEN** the change is a dated amendment linked to the contract (evidence, FR-DOC-05) and to the invoices and credit notes it causes

#### Scenario: Schedule changes from the amendment date
- **WHEN** an amendment is recorded
- **THEN** the schedule changes from the amendment date
- **AND** closed periods are never rewritten (FR-GL-12)

#### Scenario: Accounting follows the revenue policy
- **WHEN** an amendment is accounted for
- **THEN** the accounting for its kind follows the revenue policy (FR-ORG-10) [verify against the framework chosen]

### Requirement: FR-AR-16 Send quotes and invoices
The system SHALL send quotes and invoices by email with a branded PDF and a secure online view link (FR-AR-06), and SHALL let the customer accept or decline a quote online.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Quote or invoice sent by email
- **WHEN** a user sends a quote or an invoice
- **THEN** it goes out by email with a branded PDF and a secure online view link (FR-AR-06)

#### Scenario: Customer accepts or declines a quote online
- **WHEN** a customer accepts or declines a quote through its online link
- **THEN** their name, the date and their comment are recorded as evidence (FR-DOC-05)

#### Scenario: Accepted quote converts in one action
- **GIVEN** a quote the customer accepted
- **WHEN** a user converts it
- **THEN** it becomes an invoice in one action (FR-AR-02)

#### Scenario: Scheduled reminders
- **WHEN** a quote is about to expire or an invoice is unpaid
- **THEN** it gets scheduled reminders (FR-AR-08)

#### Scenario: Delivery and document status shown separately
- **WHEN** a quote or invoice is sent
- **THEN** its delivery status (sent, opened, bounced) and its own status (a quote: accepted, declined, expired; an invoice: part-paid, paid) show separately on the record and in the customer's timeline

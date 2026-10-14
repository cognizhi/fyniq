# banking/payment-providers Specification

## Purpose
How Fyniq takes in payments collected by payment providers such as Stripe and HitPay: the provider connectors, the two collection models, refunds and disputes, provider fees and their GST, and payout reconciliation through clearing accounts. Derived from PRD-001 §8.5 and PRD-001 §J.4.

## Requirements

### Requirement: FR-PSP-01 Payment-provider connectors
The system SHALL read charges, refunds, disputes, fees and payouts from payment providers (Stripe, HitPay; others later, FR-INT-25) through the provider's API and signed webhooks, and SHALL post each economic action once, whatever the events' order, repetition or delay.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Signed webhooks
- **WHEN** a provider webhook arrives
- **THEN** its signature is checked: `Stripe-Signature` HMAC for Stripe, `Hitpay-Signature` v2 for HitPay

#### Scenario: Deduplicated by event ID
- **WHEN** an event arrives whose event ID was already received
- **THEN** it is deduplicated on receipt

#### Scenario: Posting identity
- **WHEN** an event arrives
- **THEN** it only tells Fyniq which object to fetch
- **AND** postings are keyed on the provider object and the economic action (for Stripe, the balance transaction ID for money legs; the invoice ID with finalised, paid, voided or credited for invoice legs)
- **AND** the same charge reported by several events, a redelivery days later or a backfill posts once

#### Scenario: Events in any order
- **WHEN** events arrive out of order
- **THEN** Fyniq never orders them by timestamp
- **AND** it fetches missing objects from the provider's API

#### Scenario: Raw payload kept
- **WHEN** an event is received
- **THEN** its raw payload, the provider API version and the parser version are stored

#### Scenario: Unknown event type parked
- **WHEN** an event of a type the parser does not know arrives
- **THEN** it is stored and parked with ⚠ for a person to review (UC-PSP-01)
- **AND** it posts nothing

#### Scenario: Amounts are never floats
- **WHEN** a provider amount is read
- **THEN** Stripe's integer minor units are used as they are, and HitPay's JSON numbers are parsed from the raw text as exact decimals (ENG-10) ([source notes](research/bank-payments-cloud-2026-10-12.md))

#### Scenario: Daily completeness sweep
- **GIVEN** provider events can arrive late, out of order or not at all (Stripe retries delivery for up to three days and does not guarantee order)
- **WHEN** the daily completeness sweep runs through the provider's list or report API for the period
- **THEN** anything missing is fetched by its provider ID, deduplicated as above
- **AND** the connector shows the date it is complete to, as a bank account does (FR-BNK-16)
- **AND** a gap it cannot close is ⚠ (C.2 #43) [verify the list or report API HitPay offers]

#### Scenario: G4-107 Webhook endpoint down longer than the retry window
- **WHEN** the webhook endpoint is down for four days, longer than Stripe's three-day retry window
- **THEN** the daily completeness sweep finds the gap and raises ⚠ (C.2 #43)
- **AND** it fetches the missing charges, refunds and fees by ID with no duplicate posting
- **AND** the payout clearing then nets to zero (C.2 #42)

### Requirement: FR-PSP-02 Model A: Fyniq invoices, the provider collects
In Model A the system SHALL put a payment link or a PayNow QR from the provider on a Fyniq invoice (FR-AR-13) and post the payment, fee and payout through the provider clearing account.

Source: PRD-001 §8.5 · Priority M · Phase P2

#### Scenario: Payment marks the invoice paid, settlement pending
- **WHEN** the provider reports the payment of an invoice
- **THEN** the invoice is marked paid, settlement pending
- **AND** Dr provider clearing (gross) · Cr AR is posted

#### Scenario: Fee and payout
- **WHEN** the provider's fee and payout are received
- **THEN** the fee posts Dr payment fees · Cr clearing
- **AND** the payout posts Dr Bank · Cr clearing

#### Scenario: B2B invoice paid by PayNow through HitPay (UC-PSP-02)
- **GIVEN** invoice INV-0150 (S$5,450) carries a HitPay PayNow QR
- **WHEN** the customer pays
- **THEN** the connector marks the invoice paid, settlement pending and posts Dr HitPay clearing 5,450.00 · Cr AR
- **AND** the fee (0.65% + S$0.30 = S$35.73) posts Dr Payment fees · Cr HitPay clearing
- **AND** when the payout of S$5,414.27 lands next day, bank sync matches it and the clearing account nets to zero

### Requirement: FR-PSP-03 Model B: the product bills at the provider
In Model B, where Stripe Billing subscriptions or HitPay recurring billing are the system of record for self-serve customers, the system SHALL mirror customers, subscriptions and the provider's invoices read-only and book revenue gross with its service period.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Revenue gross with its service period
- **WHEN** a provider invoice is booked
- **THEN** revenue is booked gross, deferred through the schedules of FR-GL-03 for its service period

#### Scenario: MRR and customer counts
- **WHEN** MRR and customer counts are reported
- **THEN** in P2 they come from the mirrored subscriptions with FR-AR-15 and FR-KPI-04
- **AND** in P1 the KPIs use the workbook method of FR-KPI-02

#### Scenario: Per transaction or daily summary
- **WHEN** Model B postings are made
- **THEN** they post per transaction or as a daily summary per currency, as set

#### Scenario: Provider invoices as tax invoices and InvoiceNow sales
- **GIVEN** Cognizhi is GST-registered
- **WHEN** the provider issues an invoice
- **THEN** it must meet the IRAS tax-invoice particulars (FR-AR-05)
- **AND** when the InvoiceNow requirement applies, it is submitted as Cognizhi's sales (FR-TAX-10)

#### Scenario: Collection state is mirrored
- **WHEN** a provider invoice is being collected
- **THEN** Fyniq mirrors its collection state: the attempts so far, the next retry date and the final outcome (paid, uncollectible or void; the subscription past due, unpaid, cancelled or paused)

#### Scenario: Fyniq never collects
- **WHEN** a provider-billed invoice is unpaid
- **THEN** Fyniq never retries or charges it, and its own reminders (FR-AR-08) do not run on it
- **AND** MRR and churn (FR-KPI-04) follow the mirrored status (Stripe documents these fields; HitPay's equivalents [verify])

#### Scenario: G4-106 Failed subscription payment retried, then unpaid
- **WHEN** a Model B subscription invoice fails payment, Stripe retries it, then marks the subscription unpaid
- **THEN** Fyniq shows the attempt count and the next retry date, sends no reminder of its own, makes no charge and mirrors the final status
- **AND** MRR and churn follow the provider status

### Requirement: FR-PSP-07 Model B events and postings
The system SHALL treat each Model B provider invoice as a mirrored AR document and post each of its events by a fixed rule, recognising revenue by each line's service period, never by the payment date.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Finalised
- **WHEN** a provider invoice is finalised
- **THEN** it posts Dr 1100 (that customer) · Cr revenue, or 2400 for the part of each line's service period after the posting month (FR-GL-03), · Cr 2200 when registered

#### Scenario: Payment succeeded
- **WHEN** a payment succeeds
- **THEN** it posts Dr provider clearing · Cr 1100

#### Scenario: Payment failed
- **WHEN** a payment fails
- **THEN** nothing is posted and the invoice stays open and ages (FR-AR-10)
- **AND** the provider's retry status is shown, with no second retry engine in Fyniq

#### Scenario: Voided, uncollectible or credited
- **WHEN** a provider invoice is voided, marked uncollectible or credited
- **THEN** a void posts a reversal, uncollectible posts a write-off (FR-AR-17), and a credit note reverses revenue or 2400 for the credited lines
- **AND** a credit note is refunded from clearing or kept as a customer credit

#### Scenario: Proration and recognition
- **WHEN** a provider invoice is prorated
- **THEN** proration follows the provider's invoice lines and their periods
- **AND** recognition follows each line's service period, never the payment date

#### Scenario: Supported in P1
- **WHEN** a Model B subscription uses fixed-price monthly or annual plans, proration on upgrade or downgrade, cancellation at period end, credit notes or refunds
- **THEN** it is posted by these rules

#### Scenario: Unsupported pricing is parked
- **WHEN** a provider invoice has usage prices, bundles, set-up fees, trials with paid add-ons or a multi-element contract
- **THEN** it is parked with ⚠ for the accountant (revenue policy, FR-ORG-10)

#### Scenario: Daily summary keeps the subledger
- **GIVEN** the daily-summary option (FR-PSP-03) is on
- **WHEN** Model B events are posted
- **THEN** the same legs post per currency and day
- **AND** the per-invoice detail stays in the mirrored subledger, so 1100 still reconciles (INV-07)

#### Scenario: G4-040 Failed charge, retry and out-of-order redelivered events
- **GIVEN** a Stripe invoice of S$49.00 finalised 1 May whose first charge fails and whose retry succeeds 4 May
- **WHEN** `invoice.paid` arrives before `charge.succeeded`, and both are redelivered on 7 May
- **THEN** on 1 May there is one AR document (Dr 1100 49.00 · Cr revenue or 2400), open and ageing 1–4 May
- **AND** there is one settlement on 4 May (Dr clearing · Cr 1100) and one fee posting
- **AND** the second event type and the redeliveries post nothing

### Requirement: FR-PSP-04 Refunds and disputes
The system SHALL record a refund as a credit note (Model A) or by mirroring the provider's (Model B), and SHALL move a disputed amount and the dispute fee to a disputes clearing account until the dispute is won or lost.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Refund creates or mirrors a credit note
- **WHEN** a customer is refunded through the provider
- **THEN** a credit note is created in Model A, or the provider's credit note is mirrored in Model B

#### Scenario: Dispute moves to disputes clearing
- **WHEN** a customer disputes a charge
- **THEN** the funds and the dispute fee move to a disputes clearing account until the dispute is won or lost

#### Scenario: Both reverse cleanly
- **WHEN** a refund or a dispute is reversed
- **THEN** it reverses cleanly in the clearing account

#### Scenario: Refund of S$49 against clearing (UC-PSP-03)
- **WHEN** a customer is refunded S$49
- **THEN** the credit note (Model A) or the mirrored Stripe credit note (Model B) is recorded against the clearing account

#### Scenario: Dispute lost or won (UC-PSP-03)
- **GIVEN** a disputed charge whose amount and dispute fee sit in the disputes clearing account
- **WHEN** the dispute is lost
- **THEN** they go to revenue reversal and an expense
- **AND** if the dispute is won, they come back

### Requirement: FR-PSP-05 Fees and their GST
The system SHALL expense provider fees once, per transaction, from the provider's data, and SHALL claim input GST on them only where the provider charges GST and issues a tax invoice.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Which fees carry GST
- **WHEN** input GST on provider fees is considered
- **THEN** Stripe charges GST on certain fees with a monthly tax invoice
- **AND** Stripe's GST covers fees such as Connect, Terminal hardware and Radar, added to the fee; whether processing fees and HitPay's fees carry GST is not confirmed

#### Scenario: Monthly tax invoice never posts a second fee expense
- **WHEN** a provider's monthly tax invoice is matched to that month's fees
- **THEN** if the provider deducted the GST inside the fee, only a reclassification posts (Dr 2200 input GST · Cr payment fees)
- **AND** if the provider bills the GST separately, Dr 2200 · Cr AP posts (or the provider balance transaction that deducts it)

#### Scenario: Tax invoice that does not agree
- **WHEN** a provider's tax invoice does not agree to the month's fees
- **THEN** it is ⚠ (C.2 #50)

#### Scenario: Month-end provider fees and their GST (UC-PSP-04)
- **GIVEN** fees were expensed per transaction during the month
- **WHEN** the provider charges GST and issues a monthly tax invoice
- **THEN** the invoice is captured (FR-DOC-12), matched to the month's fees and claims the input GST without a second fee expense
- **AND** where there is no such invoice, the fees carry no claimable GST

#### Scenario: G4-041 Thirty Stripe charges, a monthly tax invoice and unpaid-out charges
- **GIVEN** 30 Stripe charges of S$100.00 in a month, fee 3.4% + S$0.50 = S$3.90 each (S$117.00), automatic payouts, a monthly Stripe tax invoice showing GST on some fees, and 3 charges not yet paid out at month end
- **WHEN** the month is closed
- **THEN** the fees are expensed once (S$117.00)
- **AND** the tax invoice claims its GST without a second fee expense
- **AND** each settled payout nets to zero in clearing (C.2 #42)
- **AND** the S$288.30 still in clearing (3 × S$96.10, plus any reserves) agrees to Stripe's balance not yet paid out (C.2 #50)

### Requirement: FR-PSP-06 Payout reconciliation
The system SHALL match each payout to its bank line (FR-BNK-15) and break it down into the charges, refunds, disputes and fees it settles, keeping clearing per provider and per currency.

Source: PRD-001 §8.5 · Priority M · Phase P1

#### Scenario: Payout breakdown
- **WHEN** a payout is reconciled
- **THEN** its contents come from Stripe's itemised payout-reconciliation report or HitPay's charges per payout

#### Scenario: Provider transactions stored
- **WHEN** a provider transaction is received
- **THEN** it is stored with its ID, type, gross, fee, net, currency, any conversion, the date it becomes available and the payout that settles it

#### Scenario: Each settled payout nets to zero
- **WHEN** a payout is settled
- **THEN** the clearing account must net to zero for it (check C.2 #42)
- **AND** exact matches auto-reconcile (FR-BNK-14)

#### Scenario: Month-end clearing equals the provider balance
- **WHEN** a month ends
- **THEN** the clearing balance must equal the provider's reported balance not yet paid out (unsettled charges, reserves, pending refunds and disputes; C.2 #50)

#### Scenario: Automatic, manual and instant payouts
- **WHEN** Stripe payouts are reconciled
- **THEN** the itemised report needs automatic payouts, and with manual payouts the payout's contents come from the provider's balance data
- **AND** instant payouts are not supported in P1, because Stripe cannot say which transactions they contain

#### Scenario: Negative balance or bank debit
- **WHEN** the provider balance is negative or a payout debits the bank
- **THEN** the same legs post in reverse

### Requirement: Recommended collection models
The recommended default SHALL be Stripe Billing for self-serve product subscriptions (Model B) and HitPay or a free static PayNow QR for B2B invoices (Model A) (open question 23).

Source: PRD-001 §J.4

#### Scenario: Default collection models
- **GIVEN** the provider fees of 12 Oct 2026 ([source notes](research/bank-payments-cloud-2026-10-12.md)): Stripe domestic cards 3.4% + S$0.50, PayNow 1.3%, Stripe Billing 0.7% of billing volume; HitPay PayNow QR 0.65% + S$0.30 (0.9% under S$100), domestic cards 2.8% + S$0.50, no monthly fee
- **WHEN** an organisation chooses how to collect payments
- **THEN** the recommended default is Stripe Billing for self-serve product subscriptions and HitPay or a free static PayNow QR for B2B invoices

#### Scenario: Without a connector the bank shows only net payouts
- **GIVEN** no provider connector is installed
- **WHEN** a provider pays out
- **THEN** the bank shows only the net payout, and the payouts can be matched by hand from the provider's payout report

#### Scenario: With a connector every payout is broken down
- **GIVEN** a provider connector is installed
- **WHEN** a provider pays out
- **THEN** the payout breaks down into its charges, refunds and fees
- **AND** the clearing account proves that each payout nets to zero and that what remains agrees to the provider's balance

### Requirement: Subscription billed through Stripe Billing
A self-serve subscription billed by Stripe Billing SHALL be mirrored from signed events, posted on invoice finalisation and charge success, and reconciled when its payout reaches the bank (UC-PSP-01).

Source: PRD-001 §J.4

#### Scenario: Subscription mirrored
- **WHEN** a customer subscribes at S$49 a month and Stripe Billing charges the card and issues its invoice
- **THEN** the Stripe connector receives the signed events and mirrors the customer, subscription and invoice

#### Scenario: Invoice finalised and charge succeeded
- **WHEN** Stripe finalises the invoice
- **THEN** Fyniq posts Dr 1100 AR 49.00 · Cr Revenue (or deferred revenue for the service period; GST when registered)
- **AND** when the charge succeeds it posts Dr Stripe clearing 49.00 · Cr 1100 AR, and the fee Dr Payment fees · Cr Stripe clearing

#### Scenario: Payout reconciled
- **WHEN** the payout arrives days later and bank sync brings the line
- **THEN** the itemised payout report links it to every charge, refund and fee inside it
- **AND** auto-reconcile matches Dr Bank · Cr Stripe clearing
- **AND** the charges, refunds and fees in that payout net to zero in the clearing account, and what remains agrees to Stripe's balance not yet paid out

#### Scenario: Subscription KPIs (P2)
- **WHEN** subscription KPIs are reported
- **THEN** contracted MRR, churn and ARPA come from the mirrored subscriptions (FR-KPI-04)
- **AND** booked MRR comes from the ledger (FR-KPI-02)

#### Scenario: Unknown event type
- **WHEN** an event of an unknown type arrives
- **THEN** it is parked with ⚠

#### Scenario: Payout that does not net to zero
- **WHEN** a payout does not net to zero in the clearing account
- **THEN** it is ✖, an error (check C.2 #42)

#### Scenario: Failed charge, successful retry, refund of unused service
- **WHEN** a charge fails
- **THEN** the invoice stays open in AR
- **AND** a retry that succeeds settles the same invoice once (FR-PSP-01 posting identity)
- **AND** a refund of unused service follows the provider's credit note (FR-PSP-04, FR-PSP-07)

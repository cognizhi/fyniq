# Proposal

## Why

P1 reads money in: bank files, emailed reports, Wise and Airwallex, and Stripe and HitPay as money accounts. P2 closes
the loop on the customer side and extends the feeds:

- A customer cannot pay an invoice from the invoice. Model A (PRD-001 FR-AR-13, FR-PSP-02, decided by the founders on
  16 Oct 2026, open question 23) puts a HitPay payment link or PayNow checkout on the invoice, and the payment settles
  the invoice by itself when the provider's own data makes it certain. P1 leaves a HitPay charge as an unreconciled
  line (rev 0.305).
- DBS stays on emailed reports until reconciling costs more than about 15 minutes a day (open question 24). The
  host-to-host half of phase 2 (IDEAL Connect: MT940 daily, MT942 intraday over SFTP) and the bank APIs (DBS RAPID, UOB
  API Services) need the bank's own onboarding material, which Fyniq does not have.
- A company card's lines come in by import (FR-AP-18, P1) but nothing chases the receipt (FR-AP-08).

## What Changes

- **Payment links (Model A).** A one-method port, `ports.PaymentLinks`, separate from the read-only
  `ports.PaymentProvider`, creates a HitPay payment request for an issued invoice's open balance. A new connector
  capability, `payment-link`, triggered only by a person's command (or the organisation's setting that adds a link when
  an invoice is sent). A `payment_link` row per link, one live link per invoice. The provider's `payment_request`
  webhook still only names the object: the worker fetches it by GET and mirrors its payments; auto-reconcile takes the
  charge line (the clearing account's own statement line) to the invoice the link names, once, by the posting rules that
  exist. Nothing new posts and no status is added: "paid, settlement pending" is derived from the receipt and the
  payout not yet matched.
- **Host-to-host files (IDEAL Connect).** A `ports.FileDrop` port (list and read files of a remote folder) with a local
  directory adapter, feeding `bankfile` (MT940) and a new MT942 reader whose lines are pending signals
  (FR-BNK-16), never statement lines. The SFTP adapter is `[live-verify]`.
- **Bank APIs (DBS RAPID, UOB API Services).** Not built: no official specification is public (UOB's catalogue lists
  the Accounts APIs without endpoints; DBS publishes none), and PRD rev 0.156 bars unofficial layouts. The tasks record
  the exact documents to obtain.
- **Corporate cards.** A card money account can be fed by any bank-data connector (the feed is not tied to a kind);
  receipt matching suggestions and the daily missing-receipt reminder to the cardholder (FR-AP-08).
- **Model B** (the mirror, the narrow database function, the handlers and the subscriptions screen: p1-payment-providers
  3.1, 3.2, 3.4 and 4.1 to 4.4) is built under that change, per the owner's decision of 20 Nov 2026 that it is in scope
  now, so each requirement stays in one change.

## Impact

Migrations 001050 to 001069. API additions under `/v1`. One new connector capability and one new grant: a HitPay key
that can create payment requests (HitPay documents no read-only key, so the key could already; what is new is that Fyniq
now calls it). Listed `needs-human-review`, `[owner to confirm]`.

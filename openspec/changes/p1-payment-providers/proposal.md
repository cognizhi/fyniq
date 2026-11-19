# Proposal

## Why

Cognizhi will charge self-serve customers through Stripe Billing (Model B) and may receive B2B payments through HitPay's
PayNow QR (Model A's collection side). The founders decided both on 16 Oct 2026 (PRD-001 open questions 23 and 45,
rev 0.25), and on 26 Oct 2026 that neither carries a go-live transaction (rev 0.146), so this change follows go-live.

Without a connector the bank shows only the net lump sum of each payout. Nobody can tell which charges, refunds and
fees it settles, revenue from a subscription invoice is booked by hand or not at all, and Stripe's fees are never
expensed per transaction. The books cannot then answer the question FR-PSP-06 asks: does the provider's clearing
account net to zero for every payout, and does what is left agree to the provider's own balance?

Everything this change needs from the rest of the platform has landed:
- the connector framework (manifests, state machine, dual-controlled install, secrets by reference, egress guard,
  rate limit and breaker): `p1-connector-framework`;
- money accounts of kind `payment_gateway`, the bank-data feed with cursors, continuity and duplicate checks, and the
  match, categorise and transfer commands: `p1-bank-import-reconcile`;
- sales documents with spread revenue (2400), GST, credit notes, refunds and write-offs: `p1-sales`, `p1-gst`;
- the checks engine: `p1-checks`.

## What Changes

- **A provider is a money account fed by a connector (design D1).** A Stripe or HitPay connector implements the
  existing bank-data capability, so its balance transactions arrive as the statement of a `payment_gateway` money
  account, one per provider and currency, each on its own chart account (the clearing account of FR-PSP-06). Import's
  duplicate and continuity checks, the feed's cursor and "complete to" date (FR-BNK-16) and the daily sweep come from
  what exists. No second statement pipeline is built.
- **A second capability, `payment-provider`, reads the provider's objects (design D2).** A read-only port fetches an
  invoice, subscription, customer, charge, refund, dispute, payout or balance by its provider ID, and lists them for a
  period. It has no write method: nothing in this change charges, refunds, retries or cancels (FR-INT-27, FR-PSP-03).
- **Signed webhooks with their own receiver (design D3).** `/webhooks/psp/{connector}/{id}` verifies
  `Stripe-Signature` (HMAC-SHA256 over `t.body`, five-minute tolerance) or `Hitpay-Signature` against the connector's
  secrets, stores the raw payload, the provider's API version and the parser version, deduplicates on the event ID
  (answering 2xx to a redelivery), and queues processing. An event only tells Fyniq which object to fetch. An unknown
  event type is stored and parked with a warning.
- **Posting identity (design D4).** Every posting the provider causes is keyed on the provider object and the economic
  action, in a table of its own whose unique key makes a second event, a redelivery days later or a backfill post
  nothing.
- **Money legs reuse the posting rules that exist (design D5).** A charge settles the mirrored invoice through
  `customer_receipt` into the clearing account; a fee is a categorised line to payment fees; a payout is a transfer
  from the clearing account to the bank; a refund is a credit note and a customer refund. No posting source is added,
  so the Python oracle already covers them. Fee GST and disputes are the two new rules, written with the oracle's half
  staged as the build plan says.
- **Model B mirror (design D6).** Customers, subscriptions and invoices are mirrored read-only. A finalised provider
  invoice becomes an AR document that totals to the provider's invoice or is parked; its lines spread over their
  service periods (2400) and the provider's collection state (attempts, next retry, final outcome) is shown, never acted
  on.
- **Payout reconciliation (design D7).** Each payout is stored with the balance transactions it settles. Check C.2 #42
  (a settled payout nets to zero in clearing, ✖) and C.2 #50 (month-end clearing equals the provider's balance not yet
  paid out, ⚠) run on the checks engine; a gap the sweep cannot close is C.2 #43's ⚠.
- **Screens:** `provider-payouts` and `subscriptions` (both designed), with their help keys, guide pages and compiled
  knowledge-base pages.
- **PRD revision** recording the decisions of design D1, D8 and D9, the two new chart accounts and the service-period
  rule, each tagged `[accountant to confirm]`.

Not in this change: payment links and PayNow QR creation on invoices (FR-PSP-02, FR-AR-13, P2: HitPay's charges are read
and matched by the existing reconciliation); instant and manual-payout contents beyond balance data (FR-PSP-06's
limits); usage prices, bundles, set-up fees and multi-element contracts (parked, FR-PSP-07); MRR and churn from
subscriptions (FR-KPI-04, P2); the provider's monthly tax invoice capture (needs `p1-document-capture`; FR-PSP-05's
match is built, its capture arrives with that change); any call to a real Stripe or HitPay account.

## Capabilities

### New Capabilities
- None. The requirements already sit in the spec `banking/payment-providers` (derived from PRD-001 §8.5 and J.4),
  and this change implements them; `skip_specs` is set.

### Modified Capabilities
- `banking/payment-providers`, `controls/platform-checks` and `ledger/posting-rules` gain the PRD revision's wording,
  stamped with `scripts/prdspec.py` in the same commit as the PRD row.

## Impact

- **Code:** `ports` (a read-only `PaymentProvider`), `internal/connector` (a binding builder and Runtime method),
  `internal/adapters/stripe` and `internal/adapters/hitpay`, `connectors/stripe` and `connectors/hitpay`,
  `internal/pspevents` (receiver and processor), the postgres adapter, `internal/api`, `internal/checks`, the worker's
  jobs, `apps/web` (two screens).
- **Database:** migrations 000620 to 000639 (design D10); every table has `organisation_id`, FORCE RLS and grants.
- **API:** additive operations under `/v1/payment-providers` (reads, and the mapping of a provider account to a money
  account); no write to a provider.
- **Security:** a new webhook entry point, raw payloads that hold personal data, and two connectors that hold API keys.
  Threat model updated; red-team cases for forged, replayed and oversized webhooks; no AI or MCP tool is added and the
  never-list is unchanged.
- **Outside dependencies:** a Stripe account and a HitPay account for live runs. Both are `[live-verify]`; local mode
  uses Prism mocks of the providers' contracts and recorded fixtures.

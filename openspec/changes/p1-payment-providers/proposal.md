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
  secrets, stores the raw payload sealed, the provider's API version and the parser version, deduplicates on the event ID
  (HitPay sends none: Fyniq makes one of the event's type, its object and a digest of its body; a redelivery answers 2xx),
  and queues the work. An event only tells Fyniq which object to fetch. An unknown event type is stored and parked.
- **Posting identity (design D4).** A money leg is a statement line of the provider's own reference, reconciled once; an
  invoice leg will be keyed on a unique provider invoice row. A second event, a redelivery days later or a backfill posts
  nothing.
- **The worker mirrors and the API posts (design D5).** The worker holds no right to make a document, so the hourly sweep
  mirrors the provider's balance transactions, payouts and balance, and auto-reconcile in the API posts what that makes
  certain through the rules that exist: a fee to the fee account, a dispute's money to disputes clearing, a payout as a
  transfer to the bank. A charge or a refund waits for the document it settles. No posting source is added, so the Python
  oracle already covers them; a differential scenario of a provider month proves it.
- **Model B mirror (design D6) follows go-live:** the owner decided on 19 Nov 2026 that a provider invoice is made by a narrow
  database function the worker calls. Its events are known and post nothing until then (follow-up tasks 4.1 to 4.4).
- **Payout reconciliation (design D7).** Each payout is stored with the balance transactions it settles. Check C.2 #42
  (a settled payout nets to zero in clearing, error) and C.2 #50 (month-end clearing equals the provider's balance not yet
  paid out, warning) run on the checks engine; a sweep that fell behind is C.2 #43's warning.
- **API:** the provider accounts' setup (the bank its payouts land in, the fee and disputes accounts), and reads of
  accounts, payouts with their breakdown and the parked events; the setup needs a right on "Bank data… connections".
- **Screens:** `provider-payouts` and `subscriptions` stay designed and unbuilt in this change; the API gives them
  what they need.
- **A lost dispute (D9):** a draft journal that writes it off to an expense, up to the setting `psp.dispute_expense_limit`
  (S$100.00 by default), and a person's resolution of a parked event.
- **PRD revisions 0.304 and 0.305** recording the decisions of design D1, D4, D5, D8 and D9 and the owner's of 19 Nov 2026, each
  tagged `[accountant to confirm]` where it is not final.

Not in this change: the Model B mirror and its postings (after go-live, tasks 4.1 to 4.4); a lost dispute's credit-note route (task 4.5); the two screens and the guide's pages for them; payment links and PayNow QR creation on invoices (FR-PSP-02, FR-AR-13, P2: HitPay's charges are read
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

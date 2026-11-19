# Design

## Context

See proposal.md for why. What exists (read on `origin/main`, 19 Nov 2026):
- **Connectors:** a manifest per connector (`connectors/<id>/manifest.json`, schema in
  `contracts/connectors/manifest.schema.json`) and a `connector.Binding` with builders per capability
  (`OutboundEmail`, `InboundEmail`, `BankData`, `Model`) and a `VerifyWebhook`. The manifest kind `payment-provider` is
  declared in `connector.Kinds`, but no builder, Runtime method or connector uses it. `Runtime.BankData` and
  `Runtime.VerifyWebhook` read the installed connector in the work's own unit of work, check the trigger, resolve secrets
  by pinned reference and wrap the adapter in the egress guard, rate limit and breaker. `connector.change` is dual
  controlled (Owner plus a second approver; pause is the one single-person exception).
- **Bank feeds:** `bankfeed.Syncer` pulls each money account whose source is a connector: `BankData.Transactions(account,
  cursor, from)` pages become a `bankfile.Statement` that `banking.ImportStatement` imports with duplicate and
  continuity checks, the cursor moving in the same unit of work. `bankfeed.Hooks` receives `POST /webhooks/bank/...`,
  reads at most 64 KiB, never stores the body and only queues an early sync. Money accounts have a kind
  `payment_gateway` and a source `connector`; `money_account` is unique on `(organisation_id, account_id)`, so each money
  account has a chart account of its own.
- **Commands on a line:** `journal.BankTransactions` has `MatchInvoices` (a receipt `customer_receipt@1` into the line's
  money account, no kind restriction), `Categorise` (`bank_transaction@1`, with parts and GST), `RecordTransfer` and
  `CompleteTransfer` (`bank_transfer@1`), each as the caller's principal and audited. `banking.AutoReconcile` runs the
  exact matches with the S$500 line cap and the S$2,000 rule-day cap; those caps are in `decide`, not in the commands.
- **Sales:** an invoice is a `storage.Document` whose payload is an `invoice.Draft` (customer, reference, date, lines with
  `SpreadFrom` and `SpreadMonths`, GST codes); `journal.Invoices.Create` then `Post` numbers it and posts it
  (`invoice@1`: Dr 1100 · Cr revenue or 2400 · Cr 2200). Credit notes and customer refunds are their own rules.
- **Oracle:** the Python oracle covers `invoice`, `credit_note`, `customer_receipt`, `customer_refund`,
  `bank_transaction`, `bank_transfer` and `writeoff`.
- **Standard chart:** 1050 "Payment gateway (e.g. Stripe)", 5040 "Payment processing fees", 6460 "Bank charges", 2400, 2200,
  1100. No disputes clearing or dispute-loss account.
- **Checks:** C.2 #42, #43 and #50 are in Appendix C.2 and the P1–P3 plan; the checks engine (`p1-checks`) is built.

## Goals / Non-Goals

**Goals:**
- Every charge, refund, dispute, fee and payout a provider reports is posted once, however its events arrive, and every
  payout is shown with what it settles.
- The books prove two things a person can read: each settled payout nets to zero in the provider's clearing account, and
  what is left agrees to the provider's balance.
- Nothing here can move money or change a provider account: the connector is read-only.
- It builds and tests offline against recorded fixtures and Prism mocks.

**Non-Goals:**
- Payment links, PayNow QR creation, online payment (FR-PSP-02, FR-AR-13, P2); retry, charge or refund from Fyniq.
- Instant payouts and the contents of manual payouts beyond the provider's balance data (FR-PSP-06's own limits).
- MRR, churn and customer counts from subscriptions (P2, FR-KPI-04).
- Capture of the provider's monthly tax invoice (`p1-document-capture`): the match and the ⚠ are built, the document's
  arrival is that change's.
- Any call to a real provider account: `[live-verify]`.

## Decisions

### D1. A provider is a money account fed by a connector

- Each provider and currency is a `payment_gateway` money account whose `identifier` is the provider's balance
  currency (`SGD`) and whose source is the connector. Its chart account is the clearing account (the standard 1050 for the
  first, accounts the Owner adds under it for the rest), so "clearing per provider and per currency" (FR-PSP-06) is
  the money account itself and C.2 #42 and #50 read that account's ledger balance.
- The connector's `BankData` capability returns the provider's **balance transactions** as lines, signed, in the
  currency of their balance. One balance transaction gives one line for its gross (`amount`) and, when it has a fee, a
  second line for the fee, referenced `<id>:fee`. A payout's balance transaction is one negative line.
  Line references are the provider's IDs, so the existing duplicate check is the second guard after posting identity.
- *Alternatives considered:*
  - a new `provider_transaction` posting source: duplicates the statement pipeline, the duplicate and continuity checks,
    FR-BNK-16 and the oracle's half, for the same legs;
  - one clearing account for all providers: breaks per-provider, per-currency netting (FR-PSP-06) and C.2 #50;
  - modelling the provider as a sub-ledger: no requirement asks for it.
- The balance transaction is the provider's statement, so Stripe's own rule that automatic payouts are itemised and
  manual ones are not applies as the PRD says (FR-PSP-06): contents of a manual payout come from balance data.

### D2. The port is read-only, and the manifest says so

- `ports.PaymentProvider` has methods that read: `Object(kind, id)`, `List(kind, from, to, cursor)`, `Balance(currency)`.
  It has no method that writes, and a test asserts the interface's method set and that the adapters' HTTP clients only
  issue `GET`.
- The manifest capability `payment-provider` has trigger `system-rule` ("a verified webhook, or the schedule") and
  `bank-data` has `none` as Wise's does; `fyniq_scopes` and `provider_scopes` are empty, and the Stripe secret is a
  **restricted key** with read permissions only; the setup text says so, and the connector test action calls an endpoint
  that a read-only key can reach. `[live-verify]` which Stripe permissions the list calls need.
- No AI tool and no MCP tool is added (never-list, golden rule 5). A connector never approves anything.

### D3. Webhooks have a receiver of their own

- `POST /webhooks/psp/{connector}/{id}`. The connector row is found before any tenant is set, as `bankfeed.Hooks` does; an
  unknown, disabled or uninstalled one is 404; paused ones are still accepted.
- The body is read up to 512 KiB (Stripe documents events well under it; `[verify]`) and verified on the **raw bytes**:
  - Stripe: header `Stripe-Signature: t=<unix>,v1=<hex>[,v1=...]`, `v1 = HMAC-SHA256(secret, t + "." + body)`, compared in
    constant time, and `t` within five minutes of the platform clock (`now` argument; no `time.Now`). A rotation
    overlap holds two secrets (the manifest's 24-hour overlap) and any one verifies;
  - HitPay: header `Hitpay-Signature`, HMAC-SHA256 over the raw body with the connector's salt, hex, constant-time
    compare (`research/bank-payments-cloud-2026-10-12.md`). HitPay's documentation says no timestamp is signed, so a replay
    is bounded only by the event-ID deduplication. `[verify]` the exact header and the legacy `hmac` form.
- A verified event is stored in `psp_event`, unique on `(organisation_id, connector_id, event_id)`. A duplicate is a
  2xx that changes nothing. The row holds the raw payload **sealed** with `ports.Cipher` (SEC-11: it carries a customer's
  name, email and address), its SHA-256, the provider API version (Stripe's `api_version`; HitPay's webhook version),
  the parser version (the manifest's `parser_version`), the type, the object it names, and a status:
  `received`, `processed`, `parked` or `failed`. Payloads never reach logs or AI tools (SEC-15; AI exclusion on the
  table). The event type is also an outbox item `psp.process`, queued in the same transaction.
- An event type the parser does not know is stored, set `parked` with the reason, raised as a warning for a person to
  review (UC-PSP-01), and posts nothing.
- A forged signature is 401, never stored. A body over the cap is 413. Neither says why in more words than the status.

### D4. Posting identity: the provider's reference on a line, and the one mapping of an invoice

- Every money leg is a statement line whose reference is the provider's own: a balance transaction's ID, and `<id>:fee` for
  its fee (D1). A line is reconciled once (the database moves its status forward only, and import refuses a second line of
  the same reference), so **the balance transaction ID is the money leg's posting identity** and a redelivery, a late
  event or a backfill finds the line already posted and does nothing. No separate table repeats it (decided in
  building: the first design's `psp_posting` was redundant).
- Invoice legs (task 3.2) are keyed by a unique `(connector, provider invoice ID)` row in the mirror, which names the Fyniq
  document, so the same invoice reported by several events posts once.
- Events never order anything. The processor takes an event's object ID, **fetches the object** through the port, and works
  from what it fetched, so a late `invoice.paid` before `invoice.finalised` simply finds both states on the invoice
  (G4-040). A fetched object the processor needs but cannot find is retried by the outbox with backoff, then parked.

### D5. Money legs use the rules that exist, posted by auto-reconcile

The worker holds no right to make a document (`INSERT` on `document` is the API's, and PRD-001 rev 0.143 has the bank sync
"post nothing: the lines wait for Reconcile now"), and this change does not widen a grant. So the
worker **mirrors** (D7) and the API **posts**: auto-reconcile (`banking.AutoReconcile`, which runs after an import and on a
person's Reconcile now) gains one more exact case, a line of a provider money account whose provider transaction the sweep
has mirrored. It keeps the S$500 per-line cap, the audit actor `service bank.auto-reconcile` acting for the person who
pressed the button, the preview (`Plan`), the weekly sample (FM-3) and undo, and its basis is `exact`.

| Provider fact | Posts as | Rule | When |
|---|---|---|---|
| Charge succeeded (invoice paid) | receipt of the mirrored invoice into the clearing account: Dr clearing · Cr 1100 | `customer_receipt@1` via `MatchInvoices` | task 3.2: the line's charge is mapped to one Fyniq document |
| Charge with no mirrored invoice (HitPay PayNow, a Stripe one-off) | stays an unreconciled line; auto-reconcile's existing exact match (amount, number, contact) or a person matches it | `customer_receipt@1` | existing paths |
| Fee (`<id>:fee`, or a fee-category transaction) | Dr the fee account (5040 by default) · Cr clearing, GST code `OP` (no claimable GST, D8) | `bank_transaction@1` via `Categorise` | exact, once the sweep has mirrored it |
| Payout | transfer from the clearing account to the bank account it lands in: Dr bank · Cr clearing | `bank_transfer@1` via `RecordTransfer`; the bank's own line completes it | exact; a payout above the cap waits for a person, who sees its breakdown |
| Refund | credit note on the mirrored invoice (Model B) and a customer refund from clearing | credit note + `customer_refund@1` | task 3.2 |
| Dispute opened (funds and fee withdrawn) | Dr disputes clearing · Cr clearing, GST code `NA` | `bank_transaction@1` | exact, when the account has a disputes account |
| Dispute won (funds and fee reinstated) | Dr clearing · Cr disputes clearing | `bank_transaction@1` | exact |
| Dispute lost | nothing is posted: the amount stays in disputes clearing until a person posts it (D9) | manual journal | a person |

- The audit trail names the connector and the provider's reference, and a locked period or any refusal of the rules leaves
  the line for a person with the reason, as every auto-reconcile does.
- A negative provider balance or a payout that debits the bank has the same legs reversed (FR-PSP-06): the signs of the
  lines carry it; a payout line that brings money in waits for a person.

### D6. Model B: the mirror is read-only and is an AR document (not built: decision 2 below)

The worker cannot make a document either (D5), so a mirrored Stripe invoice cannot be created where its events are read. This
section is the design the owner's decision 2 chooses a way to build; until then the events of invoices, credit notes,
subscriptions and customers are known and post nothing (the adapters list them as informational), and no collection state is
mirrored.

- `psp_customer`, `psp_subscription` and `psp_invoice` hold the provider's objects as read, with their provider IDs and
  the fetch time. `psp_invoice` also holds the **collection state**: attempt count, next retry date, final outcome
  (`paid`, `uncollectible`, `void`), the subscription's status (`past_due`, `unpaid`, `canceled`, `paused`). Nothing writes
  to the provider and Fyniq's reminders (FR-AR-08) skip documents with a provider mapping.
- A provider customer maps to a Fyniq customer (`contact`) by the provider ID, created on first sight as a draft contact
  the Owner may merge; the mapping is a row, never a free-text match.
- A finalised invoice becomes an `invoice.Draft` created and posted through `journal.Invoices.Create` and `Post`, so
  GST, rounding and numbering are the existing ones. Its **total must equal the provider's total to the cent**; if not
  (a GST rounding difference, a line type that is not supported), the invoice is parked with ⚠ for the accountant and
  nothing is posted. Usage prices, bundles, set-up fees, trials with paid add-ons and multi-element contracts park the
  same way (FR-PSP-07).
- **Service periods (decided here, `[accountant to confirm]`):** each provider line carries a period `[start, end)`. Its
  `SpreadFrom` is the month of `start` and `SpreadMonths` the number of calendar months the period touches, so a monthly
  plan is one month, an annual plan twelve, and a proration for 15 May to 1 June is one month (May). Recognition is then
  even by month (the existing spread), never by payment date. Day-weighted recognition within a month is not built.
- Voided: reversal through `Invoices.Void`. Uncollectible: a write-off (`writeoff@1`, FR-AR-17). Credit note: `CreateCredit`.
- **Daily-summary option (FR-PSP-03):** not built in this change. Per-transaction posting is the only mode, so the
  per-invoice detail and 1100's reconciliation (INV-07) are never at risk; the summary mode and its setting wait for a
  decision on how it keeps the subledger (tasks 3.5, left).

### D7. Payout reconciliation and the checks

- `psp_payout` and `psp_txn` store what FR-PSP-06 lists: the provider ID, type (`reporting_category`), gross, fee, net,
  currency, any conversion, the date it becomes available, and the payout that settles it; amounts are exact
  decimal strings in the currency's scale.
- **C.2 #42 (✖):** for a payout the bank has matched, the sum of the ledger lines of its balance transactions plus the
  payout line is zero. It is computed from the ledger (the money account's lines), not from `psp_txn`, so a missing
  balance transaction, an unposted fee or a wrong sign shows.
- **C.2 #50 (⚠):** at month end, the money account's ledger balance equals the provider's `Balance` (pending plus
  available) as stored by the daily sweep in `psp_balance`; a difference is listed by what the provider reports that the
  ledger lacks. The provider's monthly tax invoice against the month's fees is the same check's second half, built with
  the match of D8.
- **C.2 #43 (⚠):** the connector's sweep gap. The sweep is the bank feed's cursor sync run daily plus a list of invoices,
  refunds and disputes for the period fetched by ID; the feed's `LastError` and "complete to" date are already
  FR-BNK-16's.

### D8. Fees and their GST

- Fees are expensed once, per balance transaction, to 5040 (the organisation's payment-fee account is a setting
  `psp.fee_account`, 5040 by default). No GST is claimed on a processing fee: Stripe's list does not name it and HitPay
  publishes nothing (`[verify]`; PRD rev 0.19), so the default is that fees carry no claimable GST.
- A matched monthly tax invoice posts only the reclassification the PRD gives (Dr 2200 · Cr fees when the GST is inside the
  fee; Dr 2200 · Cr AP when billed apart). Its capture waits for `p1-document-capture`; the matching function and its ⚠
  are built and tested on a fixture.

### D9. A lost dispute, and the accounts `[accountant to confirm]`

- A dispute's money waits in a **disputes clearing** account the Owner names on the provider account (`psp_account`), by
  convention 1055; with none named, a dispute's lines wait for a person. The standard chart gains no row in this change (it
  is provisional until the accountant's chart review, and every added account moves its fixtures): the Owner adds 1055
  "Payment disputes clearing" and, for what is lost, 5045 "Dispute losses".
- The PRD's FR-PSP-04 says a lost dispute goes "to revenue reversal and an expense". The books do not know a dispute is lost
  until Stripe says so, and posting it needs a journal between two ledger accounts, which no auto-reconcile path makes. So
  this change posts **nothing** for a lost dispute: the amount stays in disputes clearing, where the balance sheet and the chart
  show it, and a person decides, with the accountant, between an expense (5045) and a credit note that reverses
  the revenue and its GST. This is a decision for the owner (below).

### D10. Migrations, 000620 to 000639

| No. | Content |
|---|---|
| 000620 | `psp_event` (sealed raw payload, unique event ID per connector), FORCE RLS, grants |
| 000622 | `psp_account` (a provider money account's setup and how far the sweep is complete), `psp_txn`, `psp_payout`, `psp_balance` |
| 000624 | `psp_customer`, `psp_subscription`, `psp_invoice` (collection state, the one mapping of a provider invoice to its document) |
| 000626 to 000639 | spare |

Every table has `organisation_id`, `FORCE ROW LEVEL SECURITY` and grants for `fyniq_api` and the worker role only; payloads
are sealed, so a leak of the table is not a leak of customer data. Each saved change is audited in the same transaction.
Amounts are `NUMERIC(20,4)` with a CHECK refusing NaN, and the mirror is never rewritten but for the payout that settles a
transaction (set once) and a payout's standing.

### D11. Local mode

- `contracts/partners/stripe.yaml` and `hitpay.yaml` (extracts of the providers' published contracts, written from their
  reference pages with the retrieval date; `[live-verify]` against the providers' own OpenAPI files), served by Prism in
  the `partners` profile on ports 4013 and 4014, and a row in the partners README.
- Tests use `httptest` fakes with recorded fixtures: a Stripe event set for G4-040, G4-041, G4-106, G4-107 and UC-PSP-01 to 04,
  and a HitPay charge set, in `internal/adapters/<provider>/testdata`.
- The adapters speak to the provider's base URL from the connector's configuration (`production` or `sandbox`/test mode,
  Prism in the local flavour only, by the same guard Wise uses).

### D12. Amounts

- Stripe: integer minor units, converted with the currency's scale from `money` (JPY has none; BHD has three), never `/100`.
- HitPay: JSON numbers decoded with `json.Number` from the raw text into `money.Parse`, never to a `float64`. A test fails
  the build if either adapter imports `math` for conversion or decodes into a float field.

## Decided, and decisions needed

**Decided (not re-asked):**
- Stripe Billing for Model B and HitPay for Model A's PayNow QR, both read-only connectors from P1; one adapter per
  capability (PRD-001 open questions 23 and 45, rev 0.25). After go-live (rev 0.146).
- **Who may enable a connector:** the existing `connector.change` (Owner plus a second approver, step-up; Appendix D
  "Organisation integrations"), pause by one person. No new approval path.
- **Fees:** from the provider's data, per transaction, no claimable GST without a tax invoice (FR-PSP-05).
- **Disputes, fee GST and the accounts:** the defaults of D5, D8 and D9 (a dispute's money waits in disputes clearing, a lost
  dispute posts nothing, a fee carries the GST code OP), built now, `[accountant to confirm]`. The service-period rule of D6
  waits with the Model B mirror.

**Decisions needed from the owner (with the recommendation):**
1. **A lost dispute's revenue.** Options: (a) a person posts it to an expense (5045) from disputes clearing, revenue as invoiced
   (simple, GST untouched; revenue stays overstated until credited); (b) a credit note on the invoice plus the expense (matches
   the PRD, corrects revenue and GST; needs the customer match, and more code). Nothing is posted by itself either way (D9).
   **Recommendation: (a) for a small dispute, (b) when the invoice is known and the accountant agrees; build a one-click
   step from the dispute for (b) after go-live.**
2. **How a Model B invoice is made in the books (blocks FR-PSP-03 and FR-PSP-07's postings).** The worker may not make a document
   and this change does not widen that. Options: (a) a narrow SECURITY DEFINER database function the worker calls, as
   `create_scheduled_invoice_draft` lets the scheduler make recurring invoices' drafts (automatic like recurring invoices; one
   migration and a second reviewer, since it touches the posting path; the totals-equal-or-parked rule and the service-period
   rule of D6 sit in it); (b) the API makes it on Reconcile now, like the provider's fees, from the worker's mirror (no new
   grant or function; revenue is booked only when a person presses the button or imports a statement); (c) a person enters
   the invoice by hand from the mirror (smallest; the books lag). Posting from the webhook request is rejected: it puts
   provider I/O and a posting in one request. **Recommendation: (a), after go-live, with the accountant's confirmation of
   the service-period rule; (b) if (a) is judged too wide.**
3. **Who sets a provider account up.** Built: one person with a right on "Bank data… connections" (the Owner and the
   Administrator, as for setting a connector as a money account's source, which is unattended in the same way), audited with
   the old and new accounts. Option: an approval by a second person as a bank rule's auto-apply has (a new approval object
   and its tests). The setup only chooses internal accounts, each posting stays within the per-line cap and is undone like
   any auto-reconciliation, and the connector itself was enabled under dual control. **Recommendation: keep it as built; add
   the approval if the accountant wants fee and payout accounts reviewed.**
4. **HitPay's charge leg in P1.** FR-PSP-02's automatic AR leg is P2, but HitPay's connector is P1. Options: (a) charges
   arrive as unreconciled lines on the HitPay clearing account and are matched by the existing exact-reference reconcile or
   by a person (built); (b) wait for P2. **Recommendation: (a): no new rule, nothing posts without an invoice or a person.**
5. **Which currencies.** Stripe can hold several balances. Built: a money account per currency the Owner sets up; a currency
   with none is not read into the books and shows in the provider's balance only. **Recommendation: as built.**

## Risks / Trade-offs

- A provider's API version changes a field: the parser version is stored with each event and an unknown shape parks the
  event, never posts it. Fixtures per version.
- A fetched object may differ from the event's (it changed since): the processor works from the fetch, which is the
  provider's truth, and posting identity stops a second posting.
- Auto-reconcile's S$500 cap leaves large payouts for a person: intended (money-matching stays a human step above the cap).
- The connector's principal posts without a click. It can do so only through commands that exist, into open periods, with an
  exact provider ID, and each posting is audited and appears on the reconciliation page with its basis (`exact`, the provider's own ID).
